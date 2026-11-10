package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func cloneDoc(t *testing.T, doc *policy.Document) *policy.Document {
	t.Helper()
	canonical, err := policy.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	parsed, issues := policy.ParseDocument(canonical)
	if len(issues) != 0 {
		t.Fatalf("clone issues: %+v", issues)
	}
	return parsed
}

func syncActive(t *testing.T, store *Store, orgID int64, alerts ...SnapshotAlert) map[string]string {
	t.Helper()
	return syncActiveAt(t, store, orgID, time.Now().UTC(), alerts...)
}

func syncActiveAt(t *testing.T, store *Store, orgID int64, observed time.Time, alerts ...SnapshotAlert) map[string]string {
	t.Helper()
	if err := store.SyncSnapshot(context.Background(), orgID, observed, alerts); err != nil {
		t.Fatalf("sync: %v", err)
	}
	response, err := store.GetProblems(context.Background(), orgID, ProblemQuery{View: "problems", State: "all", Sort: "started", Order: "desc", Now: observed, Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make(map[string]string, len(response.Problems))
	for _, problem := range response.Problems {
		ids[problem.Fingerprint] = problem.ID
	}
	return ids
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	return count
}

func TestPolicyBootstrapAndOrganizationIsolation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	first, err := store.GetPolicy(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || !first.DefaultsApplied || len(first.Occupancy) != 0 {
		t.Fatalf("bootstrap = %+v", first)
	}
	if countRows(t, store, `SELECT COUNT(*) FROM policy_audit WHERE org_id = 1 AND revision = 1 AND actor = 'system' AND action = 'bootstrap'`) != 1 {
		t.Fatal("bootstrap audit missing")
	}

	// A second organization deliberately holds a different document at the same
	// revision number. The compiled cache must keep the two apart.
	custom := cloneDoc(t, policy.DefaultDoc())
	custom.SeverityKey = "priority"
	custom.Levels[1].Aliases = append(custom.Levels[1].Aliases, "urgent")
	canonical, err := policy.CanonicalJSON(custom)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO policy_revisions (org_id, revision, doc_json, created_at, created_by) VALUES (2, 1, ?, ?, 'system')`, string(canonical), now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO policy_audit (org_id, revision, actor, created_at, action) VALUES (2, 1, 'system', ?, 'bootstrap')`, now); err != nil {
		t.Fatal(err)
	}

	org1Policy, err := store.GetPolicy(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	org2Policy, err := store.GetPolicy(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if org1Policy.Doc.SeverityKey != "severity" || org1Policy.Revision != 1 {
		t.Fatalf("org1 policy = %+v", org1Policy)
	}
	if org2Policy.Doc.SeverityKey != "priority" || org2Policy.Revision != 1 {
		t.Fatalf("org2 policy = %+v", org2Policy)
	}
	store.policyMu.Lock()
	org1Key, org2Key := store.policyCache[policyCacheKey(1, 1)], store.policyCache[policyCacheKey(2, 1)]
	store.policyMu.Unlock()
	if org1Key == nil || org2Key == nil || org1Key == org2Key || org1Key.Doc.SeverityKey == org2Key.Doc.SeverityKey {
		t.Fatalf("compiled cache did not separate organizations at revision 1")
	}

	org1IDs := syncActive(t, store, 1, SnapshotAlert{Fingerprint: "org1", StartsAt: "2026-01-01T00:00:00Z", Labels: map[string]string{"severity": "urgent"}})
	org2IDs := syncActive(t, store, 2, SnapshotAlert{Fingerprint: "org2", StartsAt: "2026-01-01T00:00:00Z", Labels: map[string]string{"priority": "urgent"}})
	org1Problem, err := store.GetProblemByID(ctx, 1, org1IDs["org1"])
	if err != nil || org1Problem.LevelID != "unknown" {
		t.Fatalf("org1 level = %+v, %v", org1Problem, err)
	}
	org2Problem, err := store.GetProblemByID(ctx, 2, org2IDs["org2"])
	if err != nil || org2Problem.LevelID != "critical" {
		t.Fatalf("org2 level = %+v, %v", org2Problem, err)
	}

	listNow := time.Now().UTC()
	org1List, err := store.GetProblems(ctx, 1, ProblemQuery{View: "problems", State: "all", Sort: "severity", Order: "desc", Now: listNow, Limit: 10})
	if err != nil || org1List.Summary.ByLevel["unknown"] != 1 || org1List.Summary.ByLevel["critical"] != 0 {
		t.Fatalf("org1 summary = %+v, %v", org1List.Summary.ByLevel, err)
	}
	org2List, err := store.GetProblems(ctx, 2, ProblemQuery{View: "problems", State: "all", Sort: "severity", Order: "desc", Now: listNow, Limit: 10})
	if err != nil || org2List.Summary.ByLevel["critical"] != 1 {
		t.Fatalf("org2 summary = %+v, %v", org2List.Summary.ByLevel, err)
	}
	filtered, err := store.GetProblems(ctx, 2, ProblemQuery{View: "problems", State: "all", Severity: "critical", Sort: "severity", Order: "desc", Now: listNow, Limit: 10})
	if err != nil || filtered.Total != 1 {
		t.Fatalf("org2 filter = %+v, %v", filtered, err)
	}
	filtered, err = store.GetProblems(ctx, 1, ProblemQuery{View: "problems", State: "all", Severity: "critical", Sort: "severity", Order: "desc", Now: listNow, Limit: 10})
	if err != nil || filtered.Total != 0 {
		t.Fatalf("org1 filter = %+v, %v", filtered, err)
	}
}

func TestPutPolicyStaleRevisionWritesNothing(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if _, err := store.GetPolicy(ctx, 1); err != nil {
		t.Fatal(err)
	}

	update := cloneDoc(t, policy.DefaultDoc())
	update.Levels[0].Name.EN = "Catastrophe"
	revision, err := store.PutPolicy(ctx, 1, "admin", 1, update)
	if err != nil || revision != 2 {
		t.Fatalf("put = %d, %v", revision, err)
	}
	revisions := countRows(t, store, `SELECT COUNT(*) FROM policy_revisions WHERE org_id = 1`)
	audit := countRows(t, store, `SELECT COUNT(*) FROM policy_audit WHERE org_id = 1`)
	stored, err := store.GetPolicy(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 2 || stored.DefaultsApplied {
		t.Fatalf("stored policy = %+v", stored)
	}

	_, err = store.PutPolicy(ctx, 1, "admin", 1, update)
	var stale *PolicyStaleError
	if !errors.As(err, &stale) || stale.Current != 2 {
		t.Fatalf("stale error = %v", err)
	}
	if countRows(t, store, `SELECT COUNT(*) FROM policy_revisions WHERE org_id = 1`) != revisions {
		t.Fatal("stale update wrote a revision")
	}
	if countRows(t, store, `SELECT COUNT(*) FROM policy_audit WHERE org_id = 1`) != audit {
		t.Fatal("stale update wrote an audit row")
	}
	after, err := store.GetPolicy(ctx, 1)
	if err != nil || after.Revision != 2 || after.Doc.Levels[0].Name.EN != "Catastrophe" {
		t.Fatalf("stale update changed the document: %+v %v", after, err)
	}
}

func TestPutPolicyRejectsInvalidDocumentWithoutWrites(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if _, err := store.GetPolicy(ctx, 1); err != nil {
		t.Fatal(err)
	}
	invalid := policy.DefaultDoc()
	invalid.Levels[0].Tone = "pink"
	_, err := store.PutPolicy(ctx, 1, "admin", 1, invalid)
	var validation *policy.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("invalid document error = %v", err)
	}
	if countRows(t, store, `SELECT COUNT(*) FROM policy_revisions WHERE org_id = 1`) != 1 {
		t.Fatal("invalid document wrote a revision")
	}
}

func TestOccupancyBlocksRemovingTrackedWorkflowAndState(t *testing.T) {
	ctx := context.Background()

	t.Run("workflow", func(t *testing.T) {
		store := openTestStore(t)
		ids := syncActive(t, store, 1, SnapshotAlert{Fingerprint: "fp", StartsAt: "2026-01-01T00:00:00Z", Labels: map[string]string{"severity": "critical"}})
		if _, err := startWorkflow(t, store, ctx, 1, ids["fp"], "editor", "editor", "basic", "", time.Now().UTC(), false); err != nil {
			t.Fatal(err)
		}
		withoutWorkflows := cloneDoc(t, policy.DefaultDoc())
		withoutWorkflows.Workflows = nil
		withoutWorkflows.Routing = nil
		_, err := store.PutPolicy(ctx, 1, "admin", 1, withoutWorkflows)
		var validation *policy.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("expected occupancy rejection, got %v", err)
		}
		found := false
		for _, issue := range validation.Issues {
			if issue.Code == "immutable" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected immutable issue, got %+v", validation.Issues)
		}
		if countRows(t, store, `SELECT COUNT(*) FROM policy_revisions WHERE org_id = 1`) != 1 {
			t.Fatal("occupancy rejection wrote a revision")
		}
	})

	t.Run("state", func(t *testing.T) {
		store := openTestStore(t)
		ids := syncActive(t, store, 1, SnapshotAlert{Fingerprint: "fp", StartsAt: "2026-01-01T00:00:00Z", Labels: map[string]string{"severity": "critical"}})
		if _, err := startWorkflow(t, store, ctx, 1, ids["fp"], "editor", "editor", "basic", "", time.Now().UTC(), false); err != nil {
			t.Fatal(err)
		}
		if _, err := transitionCurrent(t, store, ctx, 1, ids["fp"], "editor", "editor", "in_progress", "", time.Now().UTC(), false); err != nil {
			t.Fatal(err)
		}
		withoutState := cloneDoc(t, policy.DefaultDoc())
		workflow := &withoutState.Workflows[0]
		workflow.States = []policy.State{
			{ID: "new", Rank: 10, Name: policy.LocalizedText{RU: "Новый", EN: "New"}},
			{ID: "resolved", Rank: 30, Name: policy.LocalizedText{RU: "Решён", EN: "Resolved"}, OperatorComplete: true},
		}
		workflow.Transitions = []policy.Transition{
			{From: "new", To: "resolved", Requires: policy.Requires{}},
			{From: "resolved", To: "new", Requires: policy.Requires{Comment: true}},
		}
		_, err := store.PutPolicy(ctx, 1, "admin", 1, withoutState)
		var validation *policy.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("expected occupancy rejection, got %v", err)
		}
		if countRows(t, store, `SELECT COUNT(*) FROM policy_revisions WHERE org_id = 1`) != 1 {
			t.Fatal("occupancy rejection wrote a revision")
		}
	})
}

func TestPolicyRevisionsAreImmutableAndReadable(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if _, err := store.GetPolicy(ctx, 1); err != nil {
		t.Fatal(err)
	}
	update := cloneDoc(t, policy.DefaultDoc())
	update.SeverityKey = "priority"
	if _, err := store.PutPolicy(ctx, 1, "admin", 1, update); err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE policy_revisions SET doc_json = '{}' WHERE org_id = 1 AND revision = 1`); err == nil {
		t.Fatal("policy revision update was not rejected")
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM policy_revisions WHERE org_id = 1 AND revision = 1`); err == nil {
		t.Fatal("policy revision delete was not rejected")
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE policy_audit SET actor = 'x' WHERE org_id = 1`); err == nil {
		t.Fatal("policy audit update was not rejected")
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM policy_audit WHERE org_id = 1`); err == nil {
		t.Fatal("policy audit delete was not rejected")
	}

	revisions, err := store.PolicyRevisions(ctx, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 || revisions[0].Revision != 2 || revisions[0].Action != "update" || revisions[1].Action != "bootstrap" {
		t.Fatalf("revisions = %+v", revisions)
	}
	detail, err := store.PolicyRevision(ctx, 1, 1)
	if err != nil || detail.Doc.SeverityKey != "severity" {
		t.Fatalf("revision 1 = %+v, %v", detail, err)
	}
	if _, err := store.PolicyRevision(ctx, 1, 99); !errors.Is(err, ErrPolicyRevisionNotFound) {
		t.Fatalf("missing revision error = %v", err)
	}
}
