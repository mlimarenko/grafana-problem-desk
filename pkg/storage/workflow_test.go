package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestWorkflowRoutingUsesCurrentPolicyLevelBeforeNextSync(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id := syncActiveAt(t, store, 1, now, SnapshotAlert{
		Fingerprint: "fp-policy-reclassification", StartsAt: now.Format(time.RFC3339Nano),
		Labels: map[string]string{"severity": "critical", "priority": "disaster"},
	})["fp-policy-reclassification"]
	doc := cloneDoc(t, policy.DefaultDoc())
	doc.SeverityKey = "priority"
	strict := doc.Workflows[0]
	strict.ID = "strict"
	strict.MinimumRole = "admin"
	doc.Workflows = append(doc.Workflows, strict)
	doc.Routing = []policy.Route{{Match: policy.RouteMatch{Level: "disaster"}, Workflow: "strict"}}
	if _, err := store.PutPolicy(ctx, 1, "admin", 1, doc); err != nil {
		t.Fatal(err)
	}
	status, err := store.WorkflowStatus(ctx, 1, id, "editor", now, true)
	if err != nil {
		t.Fatal(err)
	}
	untracked, ok := status.(models.UntrackedWorkflowStatus)
	if !ok || untracked.SuggestedWorkflow != "strict" || len(untracked.AvailableWorkflows) != 1 {
		t.Fatalf("rendered routing used an old classification: %+v", status)
	}
	_, err = startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, true)
	var conflict *RoutingConflictError
	if !errors.As(err, &conflict) || conflict.Routed != "strict" {
		t.Fatalf("stale stored level bypassed current routing: %v", err)
	}
	problem, err := store.GetProblemByID(ctx, 1, id)
	if err != nil || problem.WorkflowID != nil {
		t.Fatalf("denied start changed workflow: %+v, %v", problem, err)
	}
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "", "", now, true); !errors.Is(err, ErrRoleRequired) {
		t.Fatalf("current derived level did not enforce Admin role: %v", err)
	}
}

// startWorkflow uses the current persisted revision, the normal rendered action.
func startWorkflow(t *testing.T, store *Store, ctx context.Context, orgID int64, id, actor, role, workflow, comment string, now time.Time, validated bool) (models.WorkflowMutationResponse, error) {
	t.Helper()
	snapshot, err := store.ActivePolicy(ctx, orgID)
	if err != nil {
		t.Fatalf("failed to read active policy: %v", err)
	}
	return store.StartWorkflow(ctx, orgID, id, actor, role, workflow, comment, snapshot.Revision, now, validated)
}

// transitionCurrent uses the persisted state and revision as the rendered action would.
func transitionCurrent(t *testing.T, store *Store, ctx context.Context, orgID int64, id, actor, role, to, comment string, now time.Time, validated bool) (models.WorkflowMutationResponse, error) {
	t.Helper()
	problem, err := store.GetProblemByID(ctx, orgID, id)
	if err != nil {
		return models.WorkflowMutationResponse{}, err
	}
	from := ""
	if problem.StateID != nil {
		from = *problem.StateID
	}
	snapshot, err := store.ActivePolicy(ctx, orgID)
	if err != nil {
		t.Fatalf("failed to read active policy: %v", err)
	}
	return store.TransitionWorkflow(ctx, orgID, id, actor, role, from, to, comment, snapshot.Revision, now, validated)
}

func workflowStatus(t *testing.T, store *Store, ctx context.Context, orgID int64, id, role string, now time.Time, validated bool) (any, error) {
	t.Helper()
	return store.WorkflowStatus(ctx, orgID, id, role, now, validated)
}

func countEventsOfKind(events []models.Event, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func TestWorkflowLifecycleReopenAndSourceGate(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	alert := SnapshotAlert{Fingerprint: "fp-life", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical"}, Annotations: map[string]string{}}
	id := syncActiveAt(t, store, 1, now, alert)["fp-life"]

	result, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Workflow.WorkflowID != "basic" || result.Workflow.StateID != "new" {
		t.Fatalf("start result = %+v", result.Workflow)
	}
	if result.Problem.WorkflowID == nil || *result.Problem.WorkflowID != "basic" || result.Problem.StateID == nil || *result.Problem.StateID != "new" {
		t.Fatalf("start problem = %+v", result.Problem)
	}
	events, err := store.GetEvents(ctx, 1, id)
	if err != nil || countEventsOfKind(events, "workflow_start") != 1 {
		t.Fatalf("start event = %+v, %v", events, err)
	}
	for _, event := range events {
		if event.Kind == "workflow_start" && (event.PolicyRevision == nil || *event.PolicyRevision != 1) {
			t.Fatalf("start event revision = %+v", event.PolicyRevision)
		}
	}

	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "in_progress", "", now, true); err != nil {
		t.Fatal(err)
	}

	var invalid *InvalidTransitionError
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "", now, true); !errors.As(err, &invalid) || !containsString(invalid.Missing, "ack") || !containsString(invalid.Missing, "sourceRecovered") {
		t.Fatalf("unmet gates error = %v (%+v)", err, invalid)
	}

	if _, err := store.AcknowledgeProblem(ctx, 1, id, "editor", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "", now, true); !errors.As(err, &invalid) || !containsString(invalid.Missing, "sourceRecovered") || containsString(invalid.Missing, "ack") {
		t.Fatalf("active source gate error = %v (%+v)", err, invalid)
	}

	if err := store.SyncSnapshot(ctx, 1, now, []SnapshotAlert{}); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "", now, false); !errors.Is(err, ErrSourceStateUntrusted) {
		t.Fatalf("unvalidated process gate error = %v", err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "", now, true); err != nil {
		t.Fatal(err)
	}

	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "in_progress", "", now, true); !errors.As(err, &invalid) || !containsString(invalid.Missing, "comment") {
		t.Fatalf("reopen gate error = %v (%+v)", err, invalid)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "in_progress", "reopening now", now, true); err != nil {
		t.Fatal(err)
	}

	comments, err := store.GetComments(ctx, 1, id)
	if err != nil || len(comments) != 1 || comments[0].Body != "reopening now" {
		t.Fatalf("reopen comment = %+v, %v", comments, err)
	}
	events, err = store.GetEvents(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	if countEventsOfKind(events, "workflow_transition") != 3 {
		t.Fatalf("transition events = %+v", events)
	}
	for _, event := range events {
		if event.Kind == "workflow_transition" && (event.PolicyRevision == nil || *event.PolicyRevision != 1) {
			t.Fatalf("transition event revision = %+v", event.PolicyRevision)
		}
	}
}

func TestWorkflowStartGatesDenyWithoutWrites(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	alert := SnapshotAlert{Fingerprint: "fp-gates", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical"}, Annotations: map[string]string{}}
	id := syncActiveAt(t, store, 1, now, alert)["fp-gates"]

	doc := cloneDoc(t, policy.DefaultDoc())
	doc.Workflows[0].StartRequires = policy.Requires{Ack: true, Owner: true, Comment: true, SourceRecovered: true}
	if _, err := store.PutPolicy(ctx, 1, "admin", 1, doc); err != nil {
		t.Fatal(err)
	}

	var missing *MissingWorkflowError
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, false); !errors.As(err, &missing) || len(missing.Missing) != 4 {
		t.Fatalf("start gate error = %v (%+v)", err, missing)
	}

	if _, err := store.AcknowledgeProblem(ctx, 1, id, "editor", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignProblem(ctx, 1, id, "editor", "owner"); err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := store.GetEvents(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "note", now, false); !errors.As(err, &missing) || !containsString(missing.Missing, "sourceRecovered") {
		t.Fatalf("active source start gate error = %v (%+v)", err, missing)
	}
	problem, err := store.GetProblemByID(ctx, 1, id)
	if err != nil || problem.WorkflowID != nil || problem.StateID != nil {
		t.Fatalf("denied start tracked the problem: %+v, %v", problem, err)
	}
	eventsAfter, err := store.GetEvents(ctx, 1, id)
	if err != nil || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("denied start wrote events: %d -> %d, %v", len(eventsBefore), len(eventsAfter), err)
	}
	comments, err := store.GetComments(ctx, 1, id)
	if err != nil || len(comments) != 0 {
		t.Fatalf("denied start wrote comments: %+v, %v", comments, err)
	}

	// Recovered source with a stored last_success still cannot bypass process validation.
	if err := store.SyncSnapshot(ctx, 1, now, []SnapshotAlert{}); err != nil {
		t.Fatal(err)
	}
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "note", now, false); !errors.As(err, &missing) || !containsString(missing.Missing, "sourceRecovered") {
		t.Fatalf("pre-start stored success bypassed process validation: %v (%+v)", err, missing)
	}
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "note", now, true); err != nil {
		t.Fatalf("validated start: %v", err)
	}
}

func TestUntrackedTransitionWritesNothing(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id := syncActiveAt(t, store, 1, now, SnapshotAlert{Fingerprint: "fp", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical"}})["fp"]
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "in_progress", "", now, true); !errors.Is(err, ErrUntracked) {
		t.Fatalf("untracked transition error = %v", err)
	}
	problem, err := store.GetProblemByID(ctx, 1, id)
	if err != nil || problem.WorkflowID != nil || problem.StateID != nil {
		t.Fatalf("untracked transition wrote state: %+v, %v", problem, err)
	}
	events, err := store.GetEvents(ctx, 1, id)
	if err != nil || len(events) != 0 {
		t.Fatalf("untracked transition wrote events: %+v, %v", events, err)
	}
}

func TestWorkflowPinningSurvivesRoutingEditAndRetirement(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	doc := cloneDoc(t, policy.DefaultDoc())
	alt := doc.Workflows[0]
	alt.ID = "alt"
	doc.Workflows = append(doc.Workflows, alt)
	doc.Routing = []policy.Route{{Match: policy.RouteMatch{Labels: map[string]string{"team": "payments"}}, Workflow: "alt"}}
	if revision, err := store.PutPolicy(ctx, 1, "admin", 1, doc); err != nil || revision != 2 {
		t.Fatalf("policy revision 2 = %d, %v", revision, err)
	}

	firstAlert := SnapshotAlert{Fingerprint: "fp-first", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical", "team": "payments"}}
	firstID := syncActiveAt(t, store, 1, now, firstAlert)["fp-first"]
	result, err := startWorkflow(t, store, ctx, 1, firstID, "editor", "editor", "", "", now, true)
	if err != nil || result.Workflow.WorkflowID != "alt" {
		t.Fatalf("routed start = %+v, %v", result.Workflow, err)
	}
	events, err := store.GetEvents(ctx, 1, firstID)
	if err != nil || countEventsOfKind(events, "workflow_start") != 1 {
		t.Fatalf("start events = %+v, %v", events, err)
	}
	for _, event := range events {
		if event.Kind == "workflow_start" && (event.PolicyRevision == nil || *event.PolicyRevision != 2) {
			t.Fatalf("start revision = %+v", event.PolicyRevision)
		}
	}

	rerouted := cloneDoc(t, policy.DefaultDoc())
	rerouted.Workflows = append(rerouted.Workflows, alt)
	rerouted.Routing = []policy.Route{{Match: policy.RouteMatch{Labels: map[string]string{"team": "payments"}}, Workflow: "basic"}}
	if _, err := store.PutPolicy(ctx, 1, "admin", 2, rerouted); err != nil {
		t.Fatal(err)
	}
	status, err := workflowStatus(t, store, ctx, 1, firstID, "editor", now, true)
	if err != nil {
		t.Fatal(err)
	}
	tracked, ok := status.(*models.TrackedWorkflowStatus)
	if !ok || tracked.WorkflowID != "alt" || tracked.StateID != "new" || tracked.RevisionUsed != 3 {
		t.Fatalf("pinned status = %+v", status)
	}

	// A second episode created after the routing edit follows the new rule.
	secondAlert := SnapshotAlert{Fingerprint: "fp-second", StartsAt: now.Add(time.Minute).Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical", "team": "payments"}}
	ids := syncActiveAt(t, store, 1, now.Add(time.Minute), firstAlert, secondAlert)
	secondResult, err := startWorkflow(t, store, ctx, 1, ids["fp-second"], "editor", "editor", "", "", now.Add(time.Minute), true)
	if err != nil || secondResult.Workflow.WorkflowID != "basic" {
		t.Fatalf("rerouted start = %+v, %v", secondResult.Workflow, err)
	}

	// A revision recorded out of band without the pinned workflow makes it retired.
	retired := cloneDoc(t, policy.DefaultDoc())
	retired.Workflows = []policy.Workflow{retired.Workflows[0]}
	retired.Routing = nil
	canonical, err := policy.CanonicalJSON(retired)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO policy_revisions (org_id, revision, doc_json, created_at, created_by) VALUES (1, 4, ?, ?, 'admin')`, string(canonical), stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO policy_audit (org_id, revision, actor, created_at, action) VALUES (1, 4, 'admin', ?, 'update')`, stamp); err != nil {
		t.Fatal(err)
	}
	status, err = workflowStatus(t, store, ctx, 1, firstID, "editor", now, true)
	if err != nil {
		t.Fatal(err)
	}
	tracked, ok = status.(*models.TrackedWorkflowStatus)
	if !ok || !tracked.Retired || tracked.WorkflowID != "alt" {
		t.Fatalf("retired status = %+v", status)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, firstID, "editor", "editor", "in_progress", "", now, true); !errors.Is(err, ErrWorkflowRetired) {
		t.Fatalf("retired transition error = %v", err)
	}
}

func sourceGateFixture(t *testing.T) (*Store, string, time.Time) {
	t.Helper()
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	alert := SnapshotAlert{Fingerprint: "fp-source", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical"}, Annotations: map[string]string{}}
	id := syncActiveAt(t, store, 1, now, alert)["fp-source"]
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, true); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "in_progress", "", now, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcknowledgeProblem(ctx, 1, id, "editor", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncSnapshot(ctx, 1, now, []SnapshotAlert{}); err != nil {
		t.Fatal(err)
	}
	return store, id, now
}

func setSourceStatus(t *testing.T, store *Store, lastSuccess, lastError string) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), `
		INSERT INTO source_sync_status (org_id, last_success, last_error, updated_at) VALUES (1, ?, ?, ?)
		ON CONFLICT(org_id) DO UPDATE SET last_success = excluded.last_success, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		lastSuccess, lastError, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRecoveredGateFailsClosed(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	trustedSuccess := now.Format(time.RFC3339)
	cases := []struct {
		name             string
		lastSuccess      string
		lastError        string
		processValidated bool
		wantErr          bool
	}{
		{"empty success", "", "", true, true},
		{"source error", trustedSuccess, "boom", true, true},
		{"stale success", now.Add(-2 * time.Hour).Format(time.RFC3339), "", true, true},
		{"unvalidated process", trustedSuccess, "", false, true},
		{"trusted", trustedSuccess, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, id, _ := sourceGateFixture(t)
			setSourceStatus(t, store, tc.lastSuccess, tc.lastError)
			eventsBefore, err := store.GetEvents(ctx, 1, id)
			if err != nil {
				t.Fatal(err)
			}
			_, err = transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "", now, tc.processValidated)
			if tc.wantErr {
				if !errors.Is(err, ErrSourceStateUntrusted) {
					t.Fatalf("gate error = %v, want source_state_untrusted", err)
				}
				problem, err := store.GetProblemByID(ctx, 1, id)
				if err != nil || problem.StateID == nil || *problem.StateID != "in_progress" {
					t.Fatalf("failed gate changed state: %+v, %v", problem, err)
				}
				eventsAfter, err := store.GetEvents(ctx, 1, id)
				if err != nil || len(eventsAfter) != len(eventsBefore) {
					t.Fatalf("failed gate wrote events: %d -> %d", len(eventsBefore), len(eventsAfter))
				}
				return
			}
			if err != nil {
				t.Fatalf("trusted transition: %v", err)
			}
			problem, err := store.GetProblemByID(ctx, 1, id)
			if err != nil || problem.StateID == nil || *problem.StateID != "resolved" {
				t.Fatalf("trusted transition state: %+v, %v", problem, err)
			}
		})
	}
}

func TestTransitionAtomicRollback(t *testing.T) {
	ctx := context.Background()
	store, id, now := sourceGateFixture(t)
	eventsBefore, err := store.GetEvents(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER reject_comment BEFORE INSERT ON comments BEGIN SELECT RAISE(ABORT, 'blocked comment'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "done", now, true); err == nil {
		t.Fatal("injected comment failure was not returned")
	}
	assertWorkflowRolledBack(t, store, id, eventsBefore)

	if _, err := store.db.ExecContext(ctx, `DROP TRIGGER reject_comment`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER reject_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT, 'blocked event'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "", now, true); err == nil {
		t.Fatal("injected event failure was not returned")
	}
	assertWorkflowRolledBack(t, store, id, eventsBefore)
}

func assertWorkflowRolledBack(t *testing.T, store *Store, id string, eventsBefore []models.Event) {
	t.Helper()
	problem, err := store.GetProblemByID(context.Background(), 1, id)
	if err != nil || problem.StateID == nil || *problem.StateID != "in_progress" {
		t.Fatalf("partial transition committed: %+v, %v", problem, err)
	}
	eventsAfter, err := store.GetEvents(context.Background(), 1, id)
	if err != nil || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("partial event committed: %d -> %d, %v", len(eventsBefore), len(eventsAfter), err)
	}
	comments, err := store.GetComments(context.Background(), 1, id)
	if err != nil || len(comments) != 0 {
		t.Fatalf("partial comment committed: %+v, %v", comments, err)
	}
}

func TestStartWorkflowAlreadyTrackedWritesNothing(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	id := syncActiveAt(t, store, 1, now, SnapshotAlert{Fingerprint: "fp", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical"}})["fp"]
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, true); err != nil {
		t.Fatal(err)
	}
	eventsBefore, _ := store.GetEvents(ctx, 1, id)
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, true); !errors.Is(err, ErrAlreadyTracked) {
		t.Fatalf("second start error = %v", err)
	}
	eventsAfter, _ := store.GetEvents(ctx, 1, id)
	if len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("duplicate start wrote events: %d -> %d", len(eventsBefore), len(eventsAfter))
	}
}

func TestWorkflowStatusSourceConflictWithoutStateChange(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	alert := SnapshotAlert{Fingerprint: "fp-status", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"severity": "critical"}, Annotations: map[string]string{}}
	id := syncActiveAt(t, store, 1, now, alert)["fp-status"]

	status, err := workflowStatus(t, store, ctx, 1, id, "editor", now, false)
	if err != nil {
		t.Fatal(err)
	}
	untracked, ok := status.(models.UntrackedWorkflowStatus)
	if !ok || untracked.Tracked || len(untracked.AvailableWorkflows) != 1 || untracked.SuggestedWorkflow != "" {
		t.Fatalf("untracked status = %+v", status)
	}

	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, true); err != nil {
		t.Fatal(err)
	}
	status, err = workflowStatus(t, store, ctx, 1, id, "editor", now, false)
	if err != nil {
		t.Fatal(err)
	}
	tracked, ok := status.(*models.TrackedWorkflowStatus)
	if !ok || !tracked.Tracked || tracked.StateID != "new" || tracked.RevisionUsed != 1 || len(tracked.Edges) != 1 || tracked.Edges[0].To != "in_progress" || !tracked.Edges[0].Allowed {
		t.Fatalf("tracked status = %+v", status)
	}
	if tracked.Source.Status != "active" || tracked.Source.Trusted {
		t.Fatalf("source snapshot = %+v", tracked.Source)
	}

	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "in_progress", "", now, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcknowledgeProblem(ctx, 1, id, "editor", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncSnapshot(ctx, 1, now, []SnapshotAlert{}); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "resolved", "", now, true); err != nil {
		t.Fatal(err)
	}

	// Re-activating the source must not move the operator workflow.
	if err := store.SyncSnapshot(ctx, 1, now.Add(time.Hour), []SnapshotAlert{alert}); err != nil {
		t.Fatal(err)
	}
	problem, err := store.GetProblemByID(ctx, 1, id)
	if err != nil || problem.Status != "active" || problem.StateID == nil || *problem.StateID != "resolved" {
		t.Fatalf("reactivation changed state: %+v, %v", problem, err)
	}
	status, err = workflowStatus(t, store, ctx, 1, id, "editor", now.Add(time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	tracked, ok = status.(*models.TrackedWorkflowStatus)
	if !ok || !tracked.SourceConflict || tracked.StateID != "resolved" {
		t.Fatalf("source conflict status = %+v", status)
	}
	if !strings.Contains(tracked.WorkflowID, "basic") {
		t.Fatalf("unexpected workflow id %q", tracked.WorkflowID)
	}
}
