package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConcurrentClaimsAcrossStores(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "claims.db")
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
	if err := first.SyncSnapshot(ctx, 1, time.Now(), []SnapshotAlert{{Fingerprint: "race", StartsAt: "2026-01-01T00:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	problems, _, err := getProblems(first, ctx, 1, "all", "", "", 10, 0)
	if err != nil || len(problems) != 1 {
		t.Fatalf("fixture: %v %+v", err, problems)
	}
	id := problems[0].ID
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, store := range []*Store{first, second} {
		wg.Add(1)
		go func(s *Store, actor string) {
			defer wg.Done()
			<-start
			_, err := s.ChangeOwnership(ctx, 1, id, actor, true)
			results <- err
		}(store, []string{"alice", "bob"}[i])
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrAssignmentConflict):
			conflict++
		default:
			t.Fatalf("claim error: %v", err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	events, err := first.GetEvents(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == "assign" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("assignment events = %d", count)
	}
}
