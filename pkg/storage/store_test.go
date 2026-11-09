package storage_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

func getProblems(s *storage.Store, ctx context.Context, org int64, state, severity, search string, limit, offset int) ([]models.Problem, int, error) {
	now := time.Now().UTC()
	r, err := s.GetProblems(ctx, org, storage.ProblemQuery{View: "history", State: state, Severity: severity, Search: search, Sort: "started", Order: "desc", From: now.AddDate(-200, 0, 0), To: now.AddDate(200, 0, 0), Now: now, Limit: limit, Offset: offset})
	return r.Problems, r.Total, err
}

func TestStorePersistenceAfterReopen(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "problemdesk.db")

	store1, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store1: %v", err)
	}

	orgID := int64(10)
	t1 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	alert := storage.SnapshotAlert{
		Fingerprint:  "fp-persist-1",
		StartsAt:     t1.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "DiskFull", "severity": "critical"},
		Annotations:  map[string]string{"summary": "Disk 99% full"},
		GeneratorURL: "http://prometheus/graph",
	}

	if err := store1.SyncSnapshot(ctx, orgID, t1, []storage.SnapshotAlert{alert}); err != nil {
		t.Fatalf("failed to sync snapshot: %v", err)
	}

	problems, total, err := getProblems(store1, ctx, orgID, "active", "", "", 10, 0)
	if err != nil || total != 1 || len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d, err: %v", total, err)
	}
	probID := problems[0].ID

	comm, err := store1.AddComment(ctx, orgID, probID, "Working on clearing logs", "admin")
	if err != nil {
		t.Fatalf("failed to add comment: %v", err)
	}

	if _, err := store1.AcknowledgeProblem(ctx, orgID, probID, "admin", "acknowledged disk issue"); err != nil {
		t.Fatalf("failed to ack problem: %v", err)
	}
	if _, err := store1.AssignProblem(ctx, orgID, probID, "admin", "alice"); err != nil {
		t.Fatalf("failed to assign problem: %v", err)
	}

	if err := store1.SetSyncStatus(ctx, orgID, t1.Format(time.RFC3339), ""); err != nil {
		t.Fatalf("failed to set sync status: %v", err)
	}

	// Close store1
	if err := store1.Close(); err != nil {
		t.Fatalf("failed to close store1: %v", err)
	}

	// Reopen store2 from the SAME dbPath
	store2, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store2: %v", err)
	}
	defer store2.Close()

	// Verify problem persisted
	reloaded, err := store2.GetProblemByID(ctx, orgID, probID)
	if err != nil {
		t.Fatalf("failed to load problem from store2: %v", err)
	}
	if reloaded.AcknowledgedBy != "admin" {
		t.Errorf("expected acknowledgedBy admin, got %q", reloaded.AcknowledgedBy)
	}
	if reloaded.Assignee != "alice" {
		t.Errorf("expected assignee alice, got %q", reloaded.Assignee)
	}
	if reloaded.Fingerprint != "fp-persist-1" {
		t.Errorf("expected fingerprint fp-persist-1, got %q", reloaded.Fingerprint)
	}

	// Verify comments persisted
	comments, err := store2.GetComments(ctx, orgID, probID)
	if err != nil {
		t.Fatalf("failed to get comments: %v", err)
	}
	// 2 comments: user comment + ack comment
	if len(comments) < 1 {
		t.Fatalf("expected at least 1 comment, got %d", len(comments))
	}
	found := false
	for _, c := range comments {
		if c.ID == comm.ID && c.Body == "Working on clearing logs" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("original comment not found after reopen")
	}

	// Verify events persisted
	events, err := store2.GetEvents(ctx, orgID, probID)
	if err != nil || len(events) == 0 {
		t.Fatalf("expected events after reopen, got %d, err: %v", len(events), err)
	}

	// Verify sync status persisted
	syncSt, err := store2.GetSyncStatus(ctx, orgID)
	if err != nil {
		t.Fatalf("failed to get sync status: %v", err)
	}
	if syncSt.LastSuccess != t1.Format(time.RFC3339) {
		t.Errorf("expected lastSuccess %s, got %s", t1.Format(time.RFC3339), syncSt.LastSuccess)
	}
}

func TestCrossOrgReadWriteIsolation(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	org1 := int64(1)
	org2 := int64(2)
	now := time.Now().UTC()

	alert := storage.SnapshotAlert{
		Fingerprint:  "fp-org1",
		StartsAt:     now.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "SecretOrg1Alert"},
		Annotations:  map[string]string{"summary": "Internal issue"},
		GeneratorURL: "http://internal/alert",
	}

	if err := store.SyncSnapshot(ctx, org1, now, []storage.SnapshotAlert{alert}); err != nil {
		t.Fatalf("sync org1 failed: %v", err)
	}

	probsOrg1, totalOrg1, err := getProblems(store, ctx, org1, "all", "", "", 10, 0)
	if err != nil || totalOrg1 != 1 {
		t.Fatalf("expected 1 problem in org1, got %d, err: %v", totalOrg1, err)
	}
	probID := probsOrg1[0].ID

	// 1. Cross-org listing: Org2 must see 0 problems
	probsOrg2, totalOrg2, err := getProblems(store, ctx, org2, "all", "", "", 10, 0)
	if err != nil {
		t.Fatalf("org2 query failed: %v", err)
	}
	if totalOrg2 != 0 || len(probsOrg2) != 0 {
		t.Fatalf("expected 0 problems in org2, got %d", totalOrg2)
	}

	// 2. Cross-org GET by ID: Org2 must get ErrNotFound
	if _, err := store.GetProblemByID(ctx, org2, probID); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-org GET, got %v", err)
	}

	// 3. Cross-org comment: Org2 must get ErrNotFound
	if _, err := store.AddComment(ctx, org2, probID, "cross org comment", "attacker"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-org AddComment, got %v", err)
	}

	// 4. Cross-org ack: Org2 must get ErrNotFound
	if _, err := store.AcknowledgeProblem(ctx, org2, probID, "attacker", "cross ack"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-org AcknowledgeProblem, got %v", err)
	}

	// 5. Cross-org unack: Org2 must get ErrNotFound
	if _, err := store.UnacknowledgeProblem(ctx, org2, probID, "attacker"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-org UnacknowledgeProblem, got %v", err)
	}

	// 6. Cross-org assign: Org2 must get ErrNotFound
	if _, err := store.AssignProblem(ctx, org2, probID, "attacker", "evil"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound for cross-org AssignProblem, got %v", err)
	}
}

func TestParameterizedStorageAndSQLInjection(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	orgID := int64(1)
	now := time.Now().UTC()

	// Sync normal problem
	alert := storage.SnapshotAlert{
		Fingerprint:  "fp-normal",
		StartsAt:     now.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "NormalAlert", "severity": "warning"},
		Annotations:  map[string]string{"summary": "Normal summary"},
		GeneratorURL: "http://prometheus",
	}
	_ = store.SyncSnapshot(ctx, orgID, now, []storage.SnapshotAlert{alert})

	// SQL injection attempts in search
	sqlInjections := []string{
		"' OR '1'='1",
		"'; DROP TABLE problems; --",
		"' UNION SELECT id, fingerprint, starts_at, ends_at, status, labels, annotations, generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at FROM problems --",
		"admin' --",
		`" OR "1"="1`,
	}

	for _, injection := range sqlInjections {
		_, _, err := getProblems(store, ctx, orgID, "all", "", injection, 10, 0)
		if err != nil {
			t.Errorf("search injection %q caused unexpected error: %v", injection, err)
		}

		_, _, err = getProblems(store, ctx, orgID, "all", injection, "", 10, 0)
		if err != nil {
			t.Errorf("severity injection %q caused unexpected error: %v", injection, err)
		}

		_, err = store.GetProblemByID(ctx, orgID, injection)
		if err != storage.ErrNotFound {
			t.Errorf("id injection %q expected ErrNotFound, got %v", injection, err)
		}
	}
}

func TestRecurrenceIndependentDiscussionAndAck(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	orgID := int64(1)
	t1 := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	tRecovered := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

	// Episode 1 fires at t1
	alertE1 := storage.SnapshotAlert{
		Fingerprint:  "fp-recurrent-service",
		StartsAt:     t1.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "ServiceDown", "instance": "prod-1"},
		Annotations:  map[string]string{"summary": "Instance down"},
		GeneratorURL: "http://prometheus",
	}

	if err := store.SyncSnapshot(ctx, orgID, t1, []storage.SnapshotAlert{alertE1}); err != nil {
		t.Fatalf("sync E1 failed: %v", err)
	}

	probs1, _, _ := getProblems(store, ctx, orgID, "active", "", "", 10, 0)
	if len(probs1) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(probs1))
	}
	e1ID := probs1[0].ID

	// User acks E1 and adds discussion comment
	if _, err := store.AcknowledgeProblem(ctx, orgID, e1ID, "oncall-alice", "Investigating server reboot"); err != nil {
		t.Fatalf("ack E1 failed: %v", err)
	}
	if _, err := store.AddComment(ctx, orgID, e1ID, "Found kernel panic", "oncall-alice"); err != nil {
		t.Fatalf("comment E1 failed: %v", err)
	}

	// Alert recovers: snapshot has 0 alerts at tRecovered
	if err := store.SyncSnapshot(ctx, orgID, tRecovered, []storage.SnapshotAlert{}); err != nil {
		t.Fatalf("sync recovery failed: %v", err)
	}

	p1Recovered, err := store.GetProblemByID(ctx, orgID, e1ID)
	if err != nil || p1Recovered.Status != "recovered" {
		t.Fatalf("expected E1 status recovered, got %s, err: %v", p1Recovered.Status, err)
	}
	if p1Recovered.EndsAt == "" {
		t.Fatalf("expected E1 endsAt to be set")
	}

	// Alert recurs at t2 with new startsAt
	alertE2 := storage.SnapshotAlert{
		Fingerprint:  "fp-recurrent-service",
		StartsAt:     t2.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "ServiceDown", "instance": "prod-1"},
		Annotations:  map[string]string{"summary": "Instance down"},
		GeneratorURL: "http://prometheus",
	}

	if err := store.SyncSnapshot(ctx, orgID, t2, []storage.SnapshotAlert{alertE2}); err != nil {
		t.Fatalf("sync E2 failed: %v", err)
	}

	probsActive, totalActive, err := getProblems(store, ctx, orgID, "active", "", "", 10, 0)
	if err != nil || totalActive != 1 {
		t.Fatalf("expected 1 active problem, got %d, err: %v", totalActive, err)
	}
	e2 := probsActive[0]

	// Must be distinct episode
	if e2.ID == e1ID {
		t.Fatalf("expected distinct episode ID for recurrence, got same ID %s", e2.ID)
	}

	// Recurrence must NOT inherit ACK or comments
	if e2.AcknowledgedBy != "" || e2.AcknowledgedAt != "" {
		t.Errorf("recurrent episode E2 must NOT inherit acknowledgedBy, got %q", e2.AcknowledgedBy)
	}

	e2Comments, err := store.GetComments(ctx, orgID, e2.ID)
	if err != nil {
		t.Fatalf("failed to query E2 comments: %v", err)
	}
	if len(e2Comments) != 0 {
		t.Errorf("recurrent episode E2 must have 0 comments, got %d", len(e2Comments))
	}

	// Total problems in org across all states must be 2 (E1 recovered, E2 active)
	allProbs, totalAll, err := getProblems(store, ctx, orgID, "all", "", "", 10, 0)
	if err != nil || totalAll != 2 || len(allProbs) != 2 {
		t.Fatalf("expected total 2 episodes, got %d", totalAll)
	}
}

func TestReplayIdempotence(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	orgID := int64(1)
	t1 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	tReplay := time.Date(2026, 11, 9, 10, 5, 0, 0, time.UTC)

	alert := storage.SnapshotAlert{
		Fingerprint:  "fp-replay-test",
		StartsAt:     t1.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "HighLatency"},
		Annotations:  map[string]string{"summary": "Latency p99 > 1s"},
		GeneratorURL: "http://prometheus",
	}

	// First sync
	if err := store.SyncSnapshot(ctx, orgID, t1, []storage.SnapshotAlert{alert}); err != nil {
		t.Fatalf("sync 1 failed: %v", err)
	}
	probs1, _, _ := getProblems(store, ctx, orgID, "active", "", "", 10, 0)
	eID := probs1[0].ID

	// Ack problem
	if _, err := store.AcknowledgeProblem(ctx, orgID, eID, "bob", "looking into network"); err != nil {
		t.Fatalf("ack failed: %v", err)
	}

	// Second sync with identical alert (replay)
	if err := store.SyncSnapshot(ctx, orgID, tReplay, []storage.SnapshotAlert{alert}); err != nil {
		t.Fatalf("sync 2 replay failed: %v", err)
	}

	probs2, total, err := getProblems(store, ctx, orgID, "all", "", "", 10, 0)
	if err != nil || total != 1 {
		t.Fatalf("expected total 1 problem after replay, got %d", total)
	}

	replayed := probs2[0]
	if replayed.ID != eID {
		t.Errorf("expected same ID %s after replay, got %s", eID, replayed.ID)
	}
	if replayed.AcknowledgedBy != "bob" {
		t.Errorf("replay overwrote acknowledgedBy: got %q, expected bob", replayed.AcknowledgedBy)
	}
	if replayed.LastSeenAt != tReplay.Format(time.RFC3339) {
		t.Errorf("expected lastSeenAt %s, got %s", tReplay.Format(time.RFC3339), replayed.LastSeenAt)
	}
}
