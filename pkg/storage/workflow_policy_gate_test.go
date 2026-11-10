package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

func routedPolicyDoc() *policy.Document {
	doc := policy.DefaultDoc()
	doc.Routing = []policy.Route{{
		Match:    policy.RouteMatch{Labels: map[string]string{"team": "payments"}},
		Workflow: "strict",
	}}
	doc.DefaultWorkflow = nil
	doc.Workflows = append(doc.Workflows, policy.Workflow{
		ID:            "strict",
		Name:          policy.LocalizedText{RU: "Строгий", EN: "Strict"},
		InitialState:  "new",
		MinimumRole:   policy.MinimumRoleAdmin,
		StartRequires: policy.Requires{},
		States: []policy.State{
			{ID: "new", Rank: 10, Name: policy.LocalizedText{RU: "Новый", EN: "New"}},
			{ID: "in_progress", Rank: 20, Name: policy.LocalizedText{RU: "В работе", EN: "In progress"}},
		},
		Transitions: []policy.Transition{
			{From: "new", To: "in_progress", MinimumRole: policy.MinimumRoleAdmin, Requires: policy.Requires{}},
		},
	})
	return doc
}

func assertUntrackedWithNoWrites(t *testing.T, store *Store, orgID int64, id string) {
	t.Helper()
	problem, err := store.GetProblemByID(context.Background(), orgID, id)
	if err != nil {
		t.Fatal(err)
	}
	if problem.WorkflowID != nil || problem.StateID != nil {
		t.Fatalf("denied action tracked the episode: %+v", problem)
	}
	events, err := store.GetEvents(context.Background(), orgID, id)
	if err != nil || len(events) != 0 {
		t.Fatalf("denied action wrote events: %+v, %v", events, err)
	}
	comments, err := store.GetComments(context.Background(), orgID, id)
	if err != nil || len(comments) != 0 {
		t.Fatalf("denied action wrote comments: %+v, %v", comments, err)
	}
}

func TestStartRoutingCannotBeBypassed(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	if _, err := store.PutPolicy(ctx, 1, "admin", 1, routedPolicyDoc()); err != nil {
		t.Fatal(err)
	}

	routedID := ""
	otherID := ""
	ids := syncActiveAt(t, store, 1, now,
		SnapshotAlert{
			Fingerprint: "fp-routed",
			StartsAt:    now.Format(time.RFC3339Nano),
			Labels:      map[string]string{"team": "payments", "severity": "critical"},
		},
		SnapshotAlert{
			Fingerprint: "fp-other",
			StartsAt:    now.Format(time.RFC3339Nano),
			Labels:      map[string]string{"severity": "warning"},
		},
	)
	routedID, otherID = ids["fp-routed"], ids["fp-other"]

	status, err := workflowStatus(t, store, ctx, 1, routedID, "editor", now, true)
	if err != nil {
		t.Fatal(err)
	}
	untracked, ok := status.(models.UntrackedWorkflowStatus)
	if !ok || untracked.SuggestedWorkflow != "strict" || len(untracked.AvailableWorkflows) != 1 || untracked.AvailableWorkflows[0].ID != "strict" || untracked.PolicyRevision != 2 {
		t.Fatalf("routed status = %+v", status)
	}
	status, err = workflowStatus(t, store, ctx, 1, otherID, "editor", now, true)
	if err != nil {
		t.Fatal(err)
	}
	untracked, ok = status.(models.UntrackedWorkflowStatus)
	if !ok || untracked.SuggestedWorkflow != "" || len(untracked.AvailableWorkflows) != 2 {
		t.Fatalf("unrouted status = %+v", status)
	}

	// Editor cannot substitute the unrestricted workflow for the routed strict one.
	var conflict *RoutingConflictError
	if _, err := startWorkflow(t, store, ctx, 1, routedID, "editor", "editor", "basic", "", now, true); !errors.As(err, &conflict) || conflict.Routed != "strict" {
		t.Fatalf("bypass error = %v (%+v)", err, conflict)
	}
	assertUntrackedWithNoWrites(t, store, 1, routedID)

	// Even the automatic start is denied because the routed workflow needs Admin.
	if _, err := startWorkflow(t, store, ctx, 1, routedID, "editor", "editor", "", "", now, true); !errors.Is(err, ErrRoleRequired) {
		t.Fatalf("editor routed start error = %v", err)
	}
	assertUntrackedWithNoWrites(t, store, 1, routedID)

	result, err := startWorkflow(t, store, ctx, 1, routedID, "admin", "admin", "", "", now, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Workflow.WorkflowID != "strict" || result.Workflow.StateID != "new" {
		t.Fatalf("admin routed start = %+v", result.Workflow)
	}

	// The per-transition minimum role still applies after the start.
	eventsBefore, _ := store.GetEvents(ctx, 1, routedID)
	if _, err := transitionCurrent(t, store, ctx, 1, routedID, "editor", "editor", "in_progress", "", now, true); !errors.Is(err, ErrRoleRequired) {
		t.Fatalf("editor admin-only transition error = %v", err)
	}
	problem, _ := store.GetProblemByID(ctx, 1, routedID)
	eventsAfter, _ := store.GetEvents(ctx, 1, routedID)
	if problem.StateID == nil || *problem.StateID != "new" || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("denied transition changed state/events: %+v %d -> %d", problem, len(eventsBefore), len(eventsAfter))
	}
	if _, err := transitionCurrent(t, store, ctx, 1, routedID, "admin", "admin", "in_progress", "", now, true); err != nil {
		t.Fatal(err)
	}

	// No route and no default: explicit operator choice still works for an Editor.
	result, err = startWorkflow(t, store, ctx, 1, otherID, "editor", "editor", "basic", "", now, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Workflow.WorkflowID != "basic" {
		t.Fatalf("explicit choice = %+v", result.Workflow)
	}
}

func TestWorkflowWritesRejectStaleStateAndRevision(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	id := syncActiveAt(t, store, 1, now, SnapshotAlert{
		Fingerprint: "fp-cas",
		StartsAt:    now.Format(time.RFC3339Nano),
		Labels:      map[string]string{"severity": "critical"},
	})["fp-cas"]
	if _, err := startWorkflow(t, store, ctx, 1, id, "editor", "editor", "basic", "", now, true); err != nil {
		t.Fatal(err)
	}
	if _, err := transitionCurrent(t, store, ctx, 1, id, "editor", "editor", "in_progress", "", now, true); err != nil {
		t.Fatal(err)
	}
	eventsBefore, _ := store.GetEvents(ctx, 1, id)

	// A second actor renders "new" before the first transition; the same target
	// is not reapplied under the stale source state.
	if _, err := store.TransitionWorkflow(ctx, 1, id, "editor", "editor", "new", "in_progress", "", 1, now, true); !errors.Is(err, ErrStaleState) {
		t.Fatalf("stale from error = %v", err)
	}
	problem, _ := store.GetProblemByID(ctx, 1, id)
	eventsAfter, _ := store.GetEvents(ctx, 1, id)
	if problem.StateID == nil || *problem.StateID != "in_progress" || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("stale from changed state/events: %+v %d -> %d", problem, len(eventsBefore), len(eventsAfter))
	}

	// A config edit between render and click invalidates the action even when
	// the graph would still allow the same target.
	if _, err := store.PutPolicy(ctx, 1, "admin", 1, cloneDoc(t, policy.DefaultDoc())); err != nil {
		t.Fatal(err)
	}
	var stale *StaleRevisionError
	if _, err := store.TransitionWorkflow(ctx, 1, id, "editor", "editor", "in_progress", "resolved", "", 1, now, true); !errors.As(err, &stale) || stale.Current != 2 {
		t.Fatalf("stale revision error = %v (%+v)", err, stale)
	}
	problem, _ = store.GetProblemByID(ctx, 1, id)
	eventsAfter, _ = store.GetEvents(ctx, 1, id)
	comments, _ := store.GetComments(ctx, 1, id)
	if problem.StateID == nil || *problem.StateID != "in_progress" || len(eventsAfter) != len(eventsBefore) || len(comments) != 0 {
		t.Fatalf("stale revision wrote state/events/comments: %+v %d %d", problem, len(eventsAfter), len(comments))
	}

	untrackedID := syncActiveAt(t, store, 1, now, SnapshotAlert{
		Fingerprint: "fp-cas-start",
		StartsAt:    now.Format(time.RFC3339Nano),
		Labels:      map[string]string{"severity": "warning"},
	})["fp-cas-start"]
	if _, err := store.StartWorkflow(ctx, 1, untrackedID, "editor", "editor", "basic", "", 1, now, true); !errors.As(err, &stale) || stale.Current != 2 {
		t.Fatalf("stale start revision error = %v (%+v)", err, stale)
	}
	assertUntrackedWithNoWrites(t, store, 1, untrackedID)
}
