package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
)

// supportedSchemaVersion is the newest additive schema this build understands.
const supportedSchemaVersion = 1

var (
	// ErrNotFound is returned when an episode is not found for the requested org_id.
	ErrNotFound = errors.New("problem not found")
)

// Store provides persistence for Problem Desk in pure-Go SQLite.
type Store struct {
	db *sql.DB

	policyMu    sync.Mutex
	policyCache map[string]*policySnapshot
}

// ResolveDataPath returns PROBLEMDESK_DATA_PATH or the Grafana default.
func ResolveDataPath() string {
	p := os.Getenv("PROBLEMDESK_DATA_PATH")
	if p == "" {
		p = "/var/lib/grafana/problem-desk"
	}
	return p
}

// Open initializes SQLite storage at the given path or default data directory.
func Open(customPath string) (*Store, error) {
	dbPath := customPath
	if dbPath == "" {
		dir := ResolveDataPath()
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create data directory %s: %w", dir, err)
		}
		dbPath = filepath.Join(dir, "problemdesk.db")
	} else if dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file:") {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create db directory %s: %w", dir, err)
		}
	}

	dsn := dbPath
	if !strings.Contains(dsn, "?") && dbPath != ":memory:" {
		dsn += "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Single writer connection pool for SQLite file safety
	db.SetMaxOpenConns(1)

	s := &Store{db: db, policyCache: make(map[string]*policySnapshot)}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Ping verifies database connectivity and health without tenant assumptions.
func (s *Store) Ping(ctx context.Context) error {
	if s.db == nil {
		return errors.New("database handle is nil")
	}
	return s.db.PingContext(ctx)
}

const baseSchema = `
	CREATE TABLE IF NOT EXISTS problems (
		id TEXT PRIMARY KEY,
		org_id INTEGER NOT NULL,
		fingerprint TEXT NOT NULL,
		starts_at TEXT NOT NULL,
		ends_at TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		labels TEXT NOT NULL,
		annotations TEXT NOT NULL,
		generator_url TEXT NOT NULL DEFAULT '',
		acknowledged_by TEXT NOT NULL DEFAULT '',
		acknowledged_at TEXT NOT NULL DEFAULT '',
		assignee TEXT NOT NULL DEFAULT '',
		last_seen_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(org_id, fingerprint, starts_at)
	);
	CREATE INDEX IF NOT EXISTS idx_problems_org_id ON problems(org_id, id);
	CREATE INDEX IF NOT EXISTS idx_problems_org_status ON problems(org_id, status);
	CREATE INDEX IF NOT EXISTS idx_problems_org_fp_starts ON problems(org_id, fingerprint, starts_at);

	CREATE TABLE IF NOT EXISTS comments (
		id TEXT PRIMARY KEY,
		org_id INTEGER NOT NULL,
		problem_id TEXT NOT NULL,
		body TEXT NOT NULL,
		author TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_comments_org_problem ON comments(org_id, problem_id, created_at);

	CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		org_id INTEGER NOT NULL,
		problem_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		author TEXT NOT NULL,
		created_at TEXT NOT NULL,
		body TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_events_org_problem ON events(org_id, problem_id, created_at);

	CREATE TABLE IF NOT EXISTS source_sync_status (
		org_id INTEGER PRIMARY KEY,
		last_success TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL
	);
`

const policySchema = `
	CREATE TABLE IF NOT EXISTS policy_revisions (
		org_id INTEGER NOT NULL,
		revision INTEGER NOT NULL,
		doc_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		created_by TEXT NOT NULL,
		PRIMARY KEY (org_id, revision)
	);
	CREATE TABLE IF NOT EXISTS policy_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		org_id INTEGER NOT NULL,
		revision INTEGER NOT NULL,
		actor TEXT NOT NULL,
		created_at TEXT NOT NULL,
		action TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_policy_audit_org_revision ON policy_audit(org_id, revision);
	CREATE TRIGGER IF NOT EXISTS policy_revisions_no_update
		BEFORE UPDATE ON policy_revisions
		BEGIN SELECT RAISE(ABORT, 'policy revisions are immutable'); END;
	CREATE TRIGGER IF NOT EXISTS policy_revisions_no_delete
		BEFORE DELETE ON policy_revisions
		BEGIN SELECT RAISE(ABORT, 'policy revisions are immutable'); END;
	CREATE TRIGGER IF NOT EXISTS policy_audit_no_update
		BEFORE UPDATE ON policy_audit
		BEGIN SELECT RAISE(ABORT, 'policy audit is append-only'); END;
	CREATE TRIGGER IF NOT EXISTS policy_audit_no_delete
		BEFORE DELETE ON policy_audit
		BEGIN SELECT RAISE(ABORT, 'policy audit is append-only'); END;
`

// backfillLevelSQL maps the raw default severity label to the default level id.
// It is the only migration write to existing episode rows: raw labels, comments,
// acknowledgements, owners and events stay untouched, and no workflow is invented.
const backfillLevelSQL = `
	UPDATE problems SET level_id = CASE lower(COALESCE(json_extract(labels, '$.severity'), ''))
		WHEN 'disaster' THEN 'disaster'
		WHEN 'critical' THEN 'critical'
		WHEN 'high' THEN 'high'
		WHEN 'warning' THEN 'warning'
		WHEN 'average' THEN 'warning'
		WHEN 'info' THEN 'info'
		WHEN 'information' THEN 'info'
		ELSE 'unknown' END
	WHERE level_id = ''
`

func (s *Store) migrate() error {
	ctx := context.Background()
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("failed to read schema version: %w", err)
	}
	if version > supportedSchemaVersion {
		// Fail closed: an older build must not write to a newer schema.
		return fmt.Errorf("unsupported database schema version %d: this build supports up to %d", version, supportedSchemaVersion)
	}
	if version == supportedSchemaVersion {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin schema migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, baseSchema); err != nil {
		return fmt.Errorf("failed to create base schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, policySchema); err != nil {
		return fmt.Errorf("failed to create policy schema: %w", err)
	}
	columns := []struct{ table, name, definition string }{
		{"problems", "level_id", "level_id TEXT NOT NULL DEFAULT ''"},
		{"problems", "workflow_id", "workflow_id TEXT NULL"},
		{"problems", "state_id", "state_id TEXT NULL"},
		{"events", "policy_revision", "policy_revision INTEGER NULL"},
	}
	for _, column := range columns {
		if err := ensureColumn(ctx, tx, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, backfillLevelSQL); err != nil {
		return fmt.Errorf("failed to backfill level ids: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_problems_org_level ON problems(org_id, level_id)"); err != nil {
		return fmt.Errorf("failed to index level ids: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", supportedSchemaVersion)); err != nil {
		return fmt.Errorf("failed to record schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema migration: %w", err)
	}
	return nil
}

// ensureColumn adds a column only when it is absent, making 0->1 migration idempotent.
func ensureColumn(ctx context.Context, tx *sql.Tx, table, column, definition string) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("failed to inspect table %s: %w", table, err)
	}
	exists := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("failed to inspect table %s: %w", table, err)
		}
		if name == column {
			exists = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("failed to inspect table %s: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("failed to inspect table %s: %w", table, err)
	}
	if exists {
		return nil
	}
	if _, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+definition); err != nil {
		return fmt.Errorf("failed to add column %s.%s: %w", table, column, err)
	}
	return nil
}

// GetProblemByID loads a single problem by ID, strictly enforcing org_id scoping.
func (s *Store) GetProblemByID(ctx context.Context, orgID int64, id string) (*models.Problem, error) {
	query := `
		SELECT id, fingerprint, starts_at, ends_at, status, labels, annotations,
		       generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at,
		       level_id, workflow_id, state_id
		FROM problems
		WHERE org_id = ? AND id = ?
	`
	var p models.Problem
	var labelsJSON, annotationsJSON string
	var workflowID, stateID sql.NullString
	err := s.db.QueryRowContext(ctx, query, orgID, id).Scan(
		&p.ID,
		&p.Fingerprint,
		&p.StartsAt,
		&p.EndsAt,
		&p.Status,
		&labelsJSON,
		&annotationsJSON,
		&p.GeneratorURL,
		&p.AcknowledgedBy,
		&p.AcknowledgedAt,
		&p.Assignee,
		&p.LastSeenAt,
		&p.LevelID,
		&workflowID,
		&stateID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get problem: %w", err)
	}
	if workflowID.Valid {
		value := workflowID.String
		p.WorkflowID = &value
	}
	if stateID.Valid {
		value := stateID.String
		p.StateID = &value
	}

	_ = json.Unmarshal([]byte(labelsJSON), &p.Labels)
	if p.Labels == nil {
		p.Labels = make(map[string]string)
	}
	_ = json.Unmarshal([]byte(annotationsJSON), &p.Annotations)
	if p.Annotations == nil {
		p.Annotations = make(map[string]string)
	}

	return &p, nil
}

// GetComments returns all comments for a problem, strictly scoped to org_id.
func (s *Store) GetComments(ctx context.Context, orgID int64, problemID string) ([]models.Comment, error) {
	query := `
		SELECT id, body, author, created_at
		FROM comments
		WHERE org_id = ? AND problem_id = ?
		ORDER BY created_at ASC, id ASC
	`
	rows, err := s.db.QueryContext(ctx, query, orgID, problemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query comments: %w", err)
	}
	defer rows.Close()

	comments := make([]models.Comment, 0)
	for rows.Next() {
		var c models.Comment
		if err := rows.Scan(&c.ID, &c.Body, &c.Author, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// GetEvents returns all audit events for a problem, strictly scoped to org_id.
func (s *Store) GetEvents(ctx context.Context, orgID int64, problemID string) ([]models.Event, error) {
	query := `
		SELECT id, kind, author, created_at, body, policy_revision
		FROM events
		WHERE org_id = ? AND problem_id = ?
		ORDER BY created_at ASC, id ASC
	`
	rows, err := s.db.QueryContext(ctx, query, orgID, problemID)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	events := make([]models.Event, 0)
	for rows.Next() {
		var e models.Event
		var revision sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Kind, &e.Author, &e.CreatedAt, &e.Body, &revision); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		if revision.Valid {
			value := revision.Int64
			e.PolicyRevision = &value
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// AddComment appends an organization-scoped discussion comment.
func (s *Store) AddComment(ctx context.Context, orgID int64, problemID, body, author string) (*models.Comment, error) {
	// Verify problem exists in org
	if _, err := s.GetProblemByID(ctx, orgID, problemID); err != nil {
		return nil, err
	}

	commentID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	query := `
		INSERT INTO comments (id, org_id, problem_id, body, author, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	if _, err := s.db.ExecContext(ctx, query, commentID, orgID, problemID, body, author, now); err != nil {
		return nil, fmt.Errorf("failed to insert comment: %w", err)
	}

	return &models.Comment{
		ID:        commentID,
		Body:      body,
		Author:    author,
		CreatedAt: now,
	}, nil
}

// AcknowledgeProblem marks a problem acknowledged with an audit event and optional discussion comment.
func (s *Store) AcknowledgeProblem(ctx context.Context, orgID int64, problemID, author, comment string) (*models.Problem, error) {
	p, err := s.GetProblemByID(ctx, orgID, problemID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin acknowledgement: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	updateSQL := `
		UPDATE problems
		SET acknowledged_by = ?, acknowledged_at = ?, updated_at = ?
		WHERE org_id = ? AND id = ?
	`
	if _, err := tx.ExecContext(ctx, updateSQL, author, now, now, orgID, problemID); err != nil {
		return nil, fmt.Errorf("failed to update problem ack: %w", err)
	}

	eventID := uuid.New().String()
	eventBody := "Problem acknowledged"
	if comment != "" {
		eventBody = fmt.Sprintf("Problem acknowledged with note: %s", comment)
	}
	insertEvent := `
		INSERT INTO events (id, org_id, problem_id, kind, author, created_at, body)
		VALUES (?, ?, ?, 'ack', ?, ?, ?)
	`
	if _, err := tx.ExecContext(ctx, insertEvent, eventID, orgID, problemID, author, now, eventBody); err != nil {
		return nil, fmt.Errorf("failed to record ack event: %w", err)
	}

	if comment != "" {
		commID := uuid.New().String()
		insertComm := `
			INSERT INTO comments (id, org_id, problem_id, body, author, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`
		if _, err := tx.ExecContext(ctx, insertComm, commID, orgID, problemID, comment, author, now); err != nil {
			return nil, fmt.Errorf("failed to insert ack comment: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit acknowledgement: %w", err)
	}
	p.AcknowledgedBy = author
	p.AcknowledgedAt = now
	return p, nil
}

// UnacknowledgeProblem clears acknowledgement and records an audit event.
func (s *Store) UnacknowledgeProblem(ctx context.Context, orgID int64, problemID, author string) (*models.Problem, error) {
	p, err := s.GetProblemByID(ctx, orgID, problemID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin acknowledgement removal: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	updateSQL := `
		UPDATE problems
		SET acknowledged_by = '', acknowledged_at = '', updated_at = ?
		WHERE org_id = ? AND id = ?
	`
	if _, err := tx.ExecContext(ctx, updateSQL, now, orgID, problemID); err != nil {
		return nil, fmt.Errorf("failed to clear problem ack: %w", err)
	}

	eventID := uuid.New().String()
	insertEvent := `
		INSERT INTO events (id, org_id, problem_id, kind, author, created_at, body)
		VALUES (?, ?, ?, 'unack', ?, ?, 'Problem unacknowledged')
	`
	if _, err := tx.ExecContext(ctx, insertEvent, eventID, orgID, problemID, author, now); err != nil {
		return nil, fmt.Errorf("failed to record unack event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit acknowledgement removal: %w", err)
	}
	p.AcknowledgedBy = ""
	p.AcknowledgedAt = ""
	return p, nil
}

// AssignProblem updates the assignee and records an audit event.
func (s *Store) AssignProblem(ctx context.Context, orgID int64, problemID, author, assignee string) (*models.Problem, error) {
	p, err := s.GetProblemByID(ctx, orgID, problemID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin assignment: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	updateSQL := `
		UPDATE problems
		SET assignee = ?, updated_at = ?
		WHERE org_id = ? AND id = ?
	`
	if _, err := tx.ExecContext(ctx, updateSQL, assignee, now, orgID, problemID); err != nil {
		return nil, fmt.Errorf("failed to assign problem: %w", err)
	}

	eventID := uuid.New().String()
	body := fmt.Sprintf("Assigned to %s", assignee)
	if assignee == "" {
		body = "Assignment cleared"
	}
	insertEvent := `
		INSERT INTO events (id, org_id, problem_id, kind, author, created_at, body)
		VALUES (?, ?, ?, 'assign', ?, ?, ?)
	`
	if _, err := tx.ExecContext(ctx, insertEvent, eventID, orgID, problemID, author, now, body); err != nil {
		return nil, fmt.Errorf("failed to record assign event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit assignment: %w", err)
	}
	p.Assignee = assignee
	return p, nil
}

// GetSyncStatus retrieves the last successful sync time and last error for org_id.
func (s *Store) GetSyncStatus(ctx context.Context, orgID int64) (models.SourceStatus, error) {
	query := `
		SELECT last_success, last_error
		FROM source_sync_status
		WHERE org_id = ?
	`
	var st models.SourceStatus
	err := s.db.QueryRowContext(ctx, query, orgID).Scan(&st.LastSuccess, &st.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return models.SourceStatus{LastSuccess: "", Error: ""}, nil
	}
	if err != nil {
		return models.SourceStatus{}, fmt.Errorf("failed to get sync status: %w", err)
	}
	return st, nil
}

// SetSyncStatus updates the sync status record for org_id.
func (s *Store) SetSyncStatus(ctx context.Context, orgID int64, lastSuccess, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	upsertSQL := `
		INSERT INTO source_sync_status (org_id, last_success, last_error, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(org_id) DO UPDATE SET
			last_success = CASE WHEN excluded.last_success != '' THEN excluded.last_success ELSE source_sync_status.last_success END,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, upsertSQL, orgID, lastSuccess, lastError, now)
	return err
}

// SnapshotAlert represents an alert item passed into SyncSnapshot.
type SnapshotAlert struct {
	Fingerprint  string
	StartsAt     string // Normalized RFC3339
	Labels       map[string]string
	Annotations  map[string]string
	GeneratorURL string
}

// SyncSnapshot applies a complete alert snapshot for an organization atomically.
// On success:
// - New active alerts create distinct episodes.
// - Existing active alerts update lastSeenAt (idempotent replays).
// - Active episodes previously in DB that are absent from the snapshot become recovered.
// - Last success is recorded and last error is cleared.
func (s *Store) SyncSnapshot(ctx context.Context, orgID int64, observedTime time.Time, activeAlerts []SnapshotAlert) error {
	// Resolve the active organization policy before opening the write transaction:
	// bootstrapping is a separate system write and never runs inside the snapshot tx.
	snapshot, err := s.ActivePolicy(ctx, orgID)
	if err != nil {
		return fmt.Errorf("failed to load organization policy: %w", err)
	}
	severityKey := snapshot.Doc.SeverityKey

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin snapshot transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	obsStr := observedTime.UTC().Format(time.RFC3339)

	// Set of active keys (fingerprint:startsAt) present in this snapshot
	activeKeys := make(map[string]struct{}, len(activeAlerts))

	for _, a := range activeAlerts {
		key := a.Fingerprint + ":" + a.StartsAt
		activeKeys[key] = struct{}{}

		labelsJSON, _ := json.Marshal(a.Labels)
		annotationsJSON, _ := json.Marshal(a.Annotations)
		levelID := snapshot.Compiled.ResolveLevel(a.Labels[severityKey]).Level.ID

		// Check if exists
		var existingID, existingStatus string
		checkQuery := `
			SELECT id, status
			FROM problems
			WHERE org_id = ? AND fingerprint = ? AND starts_at = ?
		`
		err := tx.QueryRowContext(ctx, checkQuery, orgID, a.Fingerprint, a.StartsAt).Scan(&existingID, &existingStatus)
		if errors.Is(err, sql.ErrNoRows) {
			// New episode
			newID := uuid.New().String()
			insertSQL := `
				INSERT INTO problems (
					id, org_id, fingerprint, starts_at, ends_at, status,
					labels, annotations, generator_url, acknowledged_by, acknowledged_at,
					assignee, last_seen_at, created_at, updated_at, level_id
				) VALUES (?, ?, ?, ?, '', 'active', ?, ?, ?, '', '', '', ?, ?, ?, ?)
			`
			if _, err := tx.ExecContext(ctx, insertSQL,
				newID, orgID, a.Fingerprint, a.StartsAt,
				string(labelsJSON), string(annotationsJSON), a.GeneratorURL,
				obsStr, obsStr, obsStr, levelID,
			); err != nil {
				return fmt.Errorf("failed to insert problem episode: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to query existing episode: %w", err)
		} else {
			// Existing episode: update last_seen_at, labels, annotations, generator_url and
			// re-resolve the derived level. Idempotent replay: DO NOT overwrite comments,
			// acknowledgedBy, workflow or state!
			updateSQL := `
				UPDATE problems
				SET last_seen_at = ?, labels = ?, annotations = ?, generator_url = ?, status = 'active', ends_at = '', updated_at = ?, level_id = ?
				WHERE org_id = ? AND id = ?
			`
			if _, err := tx.ExecContext(ctx, updateSQL,
				obsStr, string(labelsJSON), string(annotationsJSON), a.GeneratorURL, obsStr, levelID,
				orgID, existingID,
			); err != nil {
				return fmt.Errorf("failed to update problem replay: %w", err)
			}
		}
	}

	// Previously active episodes in DB that are absent from snapshot become recovered
	findActiveSQL := `
		SELECT id, fingerprint, starts_at
		FROM problems
		WHERE org_id = ? AND status = 'active'
	`
	rows, err := tx.QueryContext(ctx, findActiveSQL, orgID)
	if err != nil {
		return fmt.Errorf("failed to query active problems: %w", err)
	}

	type absentProblem struct {
		id string
	}
	var absent []absentProblem

	for rows.Next() {
		var id, fp, st string
		if err := rows.Scan(&id, &fp, &st); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan active problem: %w", err)
		}
		key := fp + ":" + st
		if _, present := activeKeys[key]; !present {
			absent = append(absent, absentProblem{id: id})
		}
	}
	rows.Close()

	for _, p := range absent {
		recoverSQL := `
			UPDATE problems
			SET status = 'recovered', ends_at = ?, updated_at = ?
			WHERE org_id = ? AND id = ?
		`
		if _, err := tx.ExecContext(ctx, recoverSQL, obsStr, obsStr, orgID, p.id); err != nil {
			return fmt.Errorf("failed to mark problem recovered: %w", err)
		}

		eventID := uuid.New().String()
		eventSQL := `
			INSERT INTO events (id, org_id, problem_id, kind, author, created_at, body)
			VALUES (?, ?, ?, 'recovered', 'system', ?, 'Alert cleared from source')
		`
		if _, err := tx.ExecContext(ctx, eventSQL, eventID, orgID, p.id, obsStr); err != nil {
			return fmt.Errorf("failed to record recovered event: %w", err)
		}
	}

	// Update source_sync_status
	syncUpsert := `
		INSERT INTO source_sync_status (org_id, last_success, last_error, updated_at)
		VALUES (?, ?, '', ?)
		ON CONFLICT(org_id) DO UPDATE SET
			last_success = excluded.last_success,
			last_error = '',
			updated_at = excluded.updated_at
	`
	if _, err := tx.ExecContext(ctx, syncUpsert, orgID, obsStr, obsStr); err != nil {
		return fmt.Errorf("failed to update sync status: %w", err)
	}

	return tx.Commit()
}
