package storage

import (
	"context"
	"testing"
	"time"
)

func TestListingUnicodeAndNanosecondBoundaries(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 123456789, time.UTC)
	unicodeAlert := SnapshotAlert{Fingerprint: "unicode", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"host": "МОСКВА%_01"}, Annotations: map[string]string{"summary": "ДИСК ЗАПОЛНЕН%_!"}}
	decoy := SnapshotAlert{Fingerprint: "decoy", StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"host": "МОСКВАxx01"}, Annotations: map[string]string{"summary": "ДИСК ЗАПОЛНЕНxx!"}}
	if err = s.SyncSnapshot(ctx, 1, now, []SnapshotAlert{unicodeAlert, decoy}); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSnapshot(ctx, 2, now, []SnapshotAlert{unicodeAlert}); err != nil {
		t.Fatal(err)
	}
	q := ProblemQuery{View: "problems", State: "all", Host: "москва%_", Search: "диск заполнен%_", Sort: "severity", Order: "desc", Now: now, Limit: 50}
	r, err := s.GetProblems(ctx, 1, q)
	if err != nil || r.Total != 1 || r.Summary.Active != 1 || r.Summary.Unacknowledged != 1 {
		t.Fatalf("unicode and org summary: %+v %v", r, err)
	}
	q.Host = "москва___"
	r, err = s.GetProblems(ctx, 1, q)
	if err != nil || r.Total != 0 {
		t.Fatalf("host wildcards must be literal: %+v %v", r, err)
	}
	q.Host, q.Search = "москва", "диск заполнен___"
	r, err = s.GetProblems(ctx, 1, q)
	if err != nil || r.Total != 0 {
		t.Fatalf("search wildcards must be literal: %+v %v", r, err)
	}

	alert := func(fp, start string) SnapshotAlert {
		return SnapshotAlert{Fingerprint: fp, StartsAt: start, Labels: map[string]string{"host": "test"}, Annotations: map[string]string{}}
	}
	alerts := []SnapshotAlert{
		alert("a", "2026-01-01T00:00:00.123456788Z"),
		alert("b", "2026-01-01T03:00:00.123456789+03:00"),
		alert("c", "2026-01-01T00:00:00.123456790Z"),
		alert("r1", "2025-12-01T00:00:00Z"),
		alert("r2", "2025-12-01T00:00:00Z"),
		alert("r3", "2025-12-01T00:00:00Z"),
	}
	if err = s.SyncSnapshot(ctx, 3, now, alerts); err != nil {
		t.Fatal(err)
	}
	for fp, end := range map[string]string{"r1": "2026-01-01T00:00:00.123456788Z", "r2": "2026-01-01T03:00:00.123456789+03:00", "r3": "2026-01-01T00:00:00.123456790Z"} {
		if _, err = s.db.ExecContext(ctx, `UPDATE problems SET status='recovered', ends_at=? WHERE org_id=? AND fingerprint=?`, end, 3, fp); err != nil {
			t.Fatal(err)
		}
	}
	q = ProblemQuery{View: "history", State: "all", From: now, To: now, Now: now, Sort: "started", Order: "desc", Limit: 50}
	r, err = s.GetProblems(ctx, 3, q)
	if err != nil || r.Total != 4 || len(r.Problems) != 4 || r.Problems[0].Fingerprint != "b" || r.Problems[1].Fingerprint != "a" {
		t.Fatalf("history boundaries and start order: %+v %v", r, err)
	}
	q.State, q.Sort = "recovered", "recovered"
	r, err = s.GetProblems(ctx, 3, q)
	if err != nil || r.Total != 2 || r.Problems[0].Fingerprint != "r3" || r.Problems[1].Fingerprint != "r2" {
		t.Fatalf("recovery boundary and order: %+v %v", r, err)
	}
	q.View, q.State = "recent", "all"
	r, err = s.GetProblems(ctx, 3, q)
	if err != nil || r.Total != 4 || r.Summary.Active != 3 || r.Summary.Recovered != 1 {
		t.Fatalf("recent boundaries: %+v %v", r, err)
	}
}
