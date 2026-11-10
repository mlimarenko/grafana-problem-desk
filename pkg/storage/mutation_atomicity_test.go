package storage

import (
	"context"
	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"testing"
	"time"
)

func getProblems(s *Store, ctx context.Context, org int64, state, severity, search string, limit, offset int) ([]models.Problem, int, error) {
	now := time.Now().UTC()
	r, err := s.GetProblems(ctx, org, ProblemQuery{View: "history", State: state, Severity: severity, Search: search, Sort: "started", Order: "desc", From: now.AddDate(-200, 0, 0), To: now.AddDate(200, 0, 0), Now: now, Limit: limit, Offset: offset})
	return r.Problems, r.Total, err
}

func TestOperatorMutationRollback(t *testing.T) {
	cases := []struct {
		name, failTable string
		prepare         func(context.Context, *Store, string) error
		mutate          func(context.Context, *Store, string) error
	}{
		{"ack event failure", "events", nil, func(ctx context.Context, s *Store, id string) error {
			_, err := s.AcknowledgeProblem(ctx, 1, id, "operator", "")
			return err
		}},
		{"ack note failure", "comments", nil, func(ctx context.Context, s *Store, id string) error {
			_, err := s.AcknowledgeProblem(ctx, 1, id, "operator", "Investigation note")
			return err
		}},
		{"unack event failure", "events", func(ctx context.Context, s *Store, id string) error {
			_, err := s.AcknowledgeProblem(ctx, 1, id, "first-operator", "")
			return err
		}, func(ctx context.Context, s *Store, id string) error {
			_, err := s.UnacknowledgeProblem(ctx, 1, id, "second-operator")
			return err
		}},
		{"owner event failure", "events", func(ctx context.Context, s *Store, id string) error {
			_, err := s.AssignProblem(ctx, 1, id, "first-operator", "old-owner")
			return err
		}, func(ctx context.Context, s *Store, id string) error {
			_, err := s.AssignProblem(ctx, 1, id, "second-operator", "new-owner")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			err = store.SyncSnapshot(ctx, 1, time.Now(), []SnapshotAlert{{Fingerprint: "incident", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo"}}})
			if err != nil {
				t.Fatal(err)
			}
			problems, _, err := getProblems(store, ctx, 1, "all", "", "", 10, 0)
			if err != nil || len(problems) != 1 {
				t.Fatalf("fixture not created: %v", err)
			}
			id := problems[0].ID
			if _, err := store.PutAssignees(ctx, 1, "admin", 0, []models.AssigneeMember{{UID: "old", Login: "old-owner"}, {UID: "new", Login: "new-owner"}}); err != nil {
				t.Fatal(err)
			}
			if tc.prepare != nil {
				if err := tc.prepare(ctx, store, id); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := store.GetProblemByID(ctx, 1, id)
			eventsBefore, _ := store.GetEvents(ctx, 1, id)
			_, err = store.db.Exec(`CREATE TRIGGER reject_fixture_write BEFORE INSERT ON ` + tc.failTable + ` BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END`)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.mutate(ctx, store, id); err == nil {
				t.Fatal("injected failure was not returned")
			}
			after, _ := store.GetProblemByID(ctx, 1, id)
			if after.AcknowledgedBy != before.AcknowledgedBy || after.AcknowledgedAt != before.AcknowledgedAt || after.Assignee != before.Assignee {
				t.Fatalf("partial operator state committed: before=%+v after=%+v", before, after)
			}
			eventsAfter, _ := store.GetEvents(ctx, 1, id)
			comments, _ := store.GetComments(ctx, 1, id)
			if len(eventsAfter) != len(eventsBefore) || len(comments) != 0 {
				t.Fatal("partial discussion or audit committed")
			}
		})
	}
}

func TestLevelFilterIsLiteral(t *testing.T) {
	ctx := context.Background()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.SyncSnapshot(ctx, 1, time.Now(), []SnapshotAlert{{Fingerprint: "incident", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo", "severity": "critical"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, total, err := getProblems(store, ctx, 1, "all", "crit%", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatal("level filter treated a literal value as a wildcard pattern")
	}
}
