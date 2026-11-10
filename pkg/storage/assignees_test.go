package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
)

func TestAssigneeCatalogCASAcrossStoreHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	start := make(chan struct{})
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		store := []*Store{first, second}[i%2]
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := store.PutAssignees(context.Background(), 1, "admin", 0,
				[]models.AssigneeMember{{UID: "one", Login: "alice"}})
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	writes := 0
	for err := range results {
		if err == nil {
			writes++
		} else if !errors.Is(err, ErrAssigneeStale) {
			t.Fatalf("concurrent writer failed outside CAS: %v", err)
		}
	}
	if writes != 1 {
		t.Fatalf("successful initial publications = %d, want 1", writes)
	}
	catalog, err := second.GetAssignees(context.Background(), 1)
	if err != nil || catalog.Revision != 1 || len(catalog.Members) != 1 {
		t.Fatalf("published catalog = %+v, %v", catalog, err)
	}
}

func TestAssigneeCatalogAndAssignment(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := s.GetAssignees(ctx, 1)
	if err != nil || c.Revision != 0 || len(c.Members) != 0 {
		t.Fatalf("initial: %+v %v", c, err)
	}
	if err := s.SyncSnapshot(ctx, 1, time.Now(), []SnapshotAlert{{Fingerprint: "f", StartsAt: "2026-01-01T00:00:00Z", Labels: map[string]string{}}}); err != nil {
		t.Fatal(err)
	}
	rows, _, err := getProblems(s, ctx, 1, "all", "", "", 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("fixture: %v", err)
	}
	id := rows[0].ID
	if _, err := s.AssignProblem(ctx, 1, id, "editor", "alice"); !errors.Is(err, ErrAssigneeUnavailable) {
		t.Fatalf("unknown: %v", err)
	}
	unknownEvents, err := s.GetEvents(ctx, 1, id)
	if err != nil || len(unknownEvents) != 0 {
		t.Fatalf("unknown wrote events: %+v %v", unknownEvents, err)
	}
	if _, err := s.PutAssignees(ctx, 1, "admin", 0, []models.AssigneeMember{{UID: "u", Login: "alice"}}); err != nil {
		t.Fatal(err)
	}
	other, err := s.GetAssignees(ctx, 2)
	if err != nil || other.Revision != 0 || len(other.Members) != 0 {
		t.Fatalf("org isolation: %+v %v", other, err)
	}
	if _, err := s.AssignProblem(ctx, 1, id, "editor", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutAssignees(ctx, 1, "admin", 0, nil); !errors.Is(err, ErrAssigneeStale) {
		t.Fatalf("stale: %v", err)
	}
	if _, err := s.PutAssignees(ctx, 1, "admin", 1, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetEvents(ctx, 1, id)
	if _, err := s.AssignProblem(ctx, 1, id, "editor", "alice"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetEvents(ctx, 1, id)
	if len(before) != len(after) {
		t.Fatal("legacy no-op wrote event")
	}
	if _, err := s.AssignProblem(ctx, 1, id, "editor", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignProblem(ctx, 1, id, "editor", "alice"); !errors.Is(err, ErrAssigneeUnavailable) {
		t.Fatalf("removed: %v", err)
	}
}
