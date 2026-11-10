package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const legacyV02Schema = `
CREATE TABLE problems (
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
CREATE TABLE comments (
	id TEXT PRIMARY KEY,
	org_id INTEGER NOT NULL,
	problem_id TEXT NOT NULL,
	body TEXT NOT NULL,
	author TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE events (
	id TEXT PRIMARY KEY,
	org_id INTEGER NOT NULL,
	problem_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	author TEXT NOT NULL,
	created_at TEXT NOT NULL,
	body TEXT NOT NULL
);
CREATE TABLE source_sync_status (
	org_id INTEGER PRIMARY KEY,
	last_success TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);
`

func openLegacyDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(legacyV02Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO problems (id, org_id, fingerprint, starts_at, ends_at, status, labels, annotations,
			generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at, created_at, updated_at)
		VALUES
		('p-warning', 1, 'fp-warning', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z', 'recovered',
		 '{"severity":"average","host":"h1"}', '{"summary":"avg"}', 'http://prom', 'alice', '2026-01-02T00:00:00Z',
		 'bob', '2026-01-02T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z'),
		('p-critical', 1, 'fp-critical', '2026-01-03T00:00:00Z', '', 'active',
		 '{"severity":"Critical"}', '{"summary":"crit"}', '', '', '', 'carol',
		 '2026-01-03T00:00:00Z', '2026-01-03T00:00:00Z', '2026-01-03T00:00:00Z'),
		('p-none', 2, 'fp-none', '2026-01-04T00:00:00Z', '', 'active',
		 '{"host":"h2"}', '{"summary":"none"}', '', '', '', '',
		 '2026-01-04T00:00:00Z', '2026-01-04T00:00:00Z', '2026-01-04T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO comments (id, org_id, problem_id, body, author, created_at)
		VALUES ('c1', 1, 'p-warning', 'legacy note', 'alice', '2026-01-02T01:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, org_id, problem_id, kind, author, created_at, body)
		VALUES ('e1', 1, 'p-warning', 'ack', 'alice', '2026-01-02T01:00:00Z', 'Problem acknowledged'),
		       ('e2', 1, 'p-warning', 'recovered', 'system', '2026-01-02T00:00:00Z', 'Alert cleared from source')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func problemSnapshot(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var snapshot string
	if err := db.QueryRow(`
		SELECT json_array(id, fingerprint, starts_at, ends_at, status, labels, annotations,
		                  generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at, created_at, updated_at)
		FROM problems WHERE id = ?`, id).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestLegacyV02MigrationPreservesRowsAndBackfillsLevel(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openLegacyDatabase(t, path)
	before := make(map[string]string)
	for _, id := range []string{"p-warning", "p-critical", "p-none"} {
		before[id] = problemSnapshot(t, legacy, id)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	defer store.Close()

	var version int
	if err := store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("user_version = %d, want 1", version)
	}

	expectedLevels := map[string]string{"p-warning": "warning", "p-critical": "critical", "p-none": "unknown"}
	for id, want := range expectedLevels {
		if after := problemSnapshot(t, store.db, id); after != before[id] {
			t.Fatalf("legacy row %s changed: before=%s after=%s", id, before[id], after)
		}
		var levelID string
		var workflowID, stateID sql.NullString
		if err := store.db.QueryRowContext(ctx, `SELECT level_id, workflow_id, state_id FROM problems WHERE id = ?`, id).Scan(&levelID, &workflowID, &stateID); err != nil {
			t.Fatal(err)
		}
		if levelID != want {
			t.Errorf("level_id %s = %q, want %q", id, levelID, want)
		}
		if workflowID.Valid || stateID.Valid {
			t.Errorf("legacy row %s must stay untracked", id)
		}
	}

	comments, err := store.GetComments(ctx, 1, "p-warning")
	if err != nil || len(comments) != 1 || comments[0].Body != "legacy note" || comments[0].Author != "alice" {
		t.Fatalf("comments preserved = %+v, %v", comments, err)
	}
	events, err := store.GetEvents(ctx, 1, "p-warning")
	if err != nil || len(events) != 2 {
		t.Fatalf("events preserved = %+v, %v", events, err)
	}
	for _, event := range events {
		if event.PolicyRevision != nil {
			t.Fatalf("legacy event gained a policy revision: %+v", event)
		}
	}

	var policyTables int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('policy_revisions', 'policy_audit')`).Scan(&policyTables); err != nil {
		t.Fatal(err)
	}
	if policyTables != 2 {
		t.Fatalf("policy tables = %d, want 2", policyTables)
	}

	// Reopening a migrated database is a no-op, not a second migration.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer reopened.Close()
	if err := reopened.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("version after reopen = %d, %v", version, err)
	}
	problem, err := reopened.GetProblemByID(ctx, 1, "p-warning")
	if err != nil || problem.LevelID != "warning" || problem.WorkflowID != nil {
		t.Fatalf("reopened problem = %+v, %v", problem, err)
	}
}

func TestFutureSchemaVersionFailsClosed(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE problems (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "unsupported database schema version") {
		t.Fatalf("future schema error = %v", err)
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var version int
	if err := check.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatalf("future version changed to %d, %v", version, err)
	}
	var tables int
	if err := check.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'policy_revisions'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("failed open wrote policy tables")
	}
}
