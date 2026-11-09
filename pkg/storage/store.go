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
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
)

var (
	// ErrNotFound is returned when an episode is not found for the requested org_id.
	ErrNotFound = errors.New("problem not found")
)

// Store provides persistence for Problem Desk in pure-Go SQLite.
type Store struct {
	db *sql.DB
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

	s := &Store{db: db}
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

func (s *Store) migrate() error {
	schema := `
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
	_, err := s.db.Exec(schema)
	return err
}

// GetProblems queries problems for the given org_id with filtering and pagination.
func (s *Store) GetProblems(ctx context.Context, orgID int64, state, severity, search string, limit, offset int) ([]models.Problem, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	whereClauses := []string{"org_id = ?"}
	args := []any{orgID}

	switch strings.ToLower(state) {
	case "recovered":
		whereClauses = append(whereClauses, "status = 'recovered'")
	case "all":
		// no status restriction
	case "active":
		fallthrough
	default:
		whereClauses = append(whereClauses, "status = 'active'")
	}

	if severity != "" {
		whereClauses = append(whereClauses, "json_extract(labels, '$.severity') = ?")
		args = append(args, severity)
	}

	if search != "" {
		likeTerm := "%" + search + "%"
		whereClauses = append(whereClauses, "(fingerprint LIKE ? OR labels LIKE ? OR annotations LIKE ? OR generator_url LIKE ?)")
		args = append(args, likeTerm, likeTerm, likeTerm, likeTerm)
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM problems WHERE %s", whereSQL)
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count problems: %w", err)
	}

	selectQuery := fmt.Sprintf(`
		SELECT id, fingerprint, starts_at, ends_at, status, labels, annotations,
		       generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at
		FROM problems
		WHERE %s
		ORDER BY starts_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, whereSQL)

	selectArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, selectQuery, selectArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query problems: %w", err)
	}
	defer rows.Close()

	problems := make([]models.Problem, 0)
	for rows.Next() {
		var p models.Problem
		var labelsJSON, annotationsJSON string
		if err := rows.Scan(
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
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan problem: %w", err)
		}
		_ = json.Unmarshal([]byte(labelsJSON), &p.Labels)
		if p.Labels == nil {
			p.Labels = make(map[string]string)
		}
		_ = json.Unmarshal([]byte(annotationsJSON), &p.Annotations)
		if p.Annotations == nil {
			p.Annotations = make(map[string]string)
		}
		problems = append(problems, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("rows error: %w", err)
	}

	return problems, total, nil
}

// GetProblemByID loads a single problem by ID, strictly enforcing org_id scoping.
func (s *Store) GetProblemByID(ctx context.Context, orgID int64, id string) (*models.Problem, error) {
	query := `
		SELECT id, fingerprint, starts_at, ends_at, status, labels, annotations,
		       generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at
		FROM problems
		WHERE org_id = ? AND id = ?
	`
	var p models.Problem
	var labelsJSON, annotationsJSON string
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get problem: %w", err)
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
		SELECT id, kind, author, created_at, body
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
		if err := rows.Scan(&e.ID, &e.Kind, &e.Author, &e.CreatedAt, &e.Body); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
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
					assignee, last_seen_at, created_at, updated_at
				) VALUES (?, ?, ?, ?, '', 'active', ?, ?, ?, '', '', '', ?, ?, ?)
			`
			if _, err := tx.ExecContext(ctx, insertSQL,
				newID, orgID, a.Fingerprint, a.StartsAt,
				string(labelsJSON), string(annotationsJSON), a.GeneratorURL,
				obsStr, obsStr, obsStr,
			); err != nil {
				return fmt.Errorf("failed to insert problem episode: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to query existing episode: %w", err)
		} else {
			// Existing episode: update last_seen_at, labels, annotations, generator_url
			// Idempotent replay: DO NOT overwrite comments or acknowledgedBy!
			updateSQL := `
				UPDATE problems
				SET last_seen_at = ?, labels = ?, annotations = ?, generator_url = ?, status = 'active', ends_at = '', updated_at = ?
				WHERE org_id = ? AND id = ?
			`
			if _, err := tx.ExecContext(ctx, updateSQL,
				obsStr, string(labelsJSON), string(annotationsJSON), a.GeneratorURL, obsStr,
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
