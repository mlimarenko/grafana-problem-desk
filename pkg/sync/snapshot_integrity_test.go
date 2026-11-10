package sync_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
	problemsync "github.com/mlimarenko/grafana-problem-desk/pkg/sync"
)

func TestSnapshotCompleteness(t *testing.T) {
	const alert = `{"fingerprint":"keep","startsAt":"2026-11-09T00:00:00.123Z","status":{"state":"active"},"labels":{"alertname":"Demo"}}`
	cases := []struct {
		name, org, alerts     string
		wantError, wantActive bool
	}{
		{"null is not an empty snapshot", `{"id":1}`, `null`, true, true},
		{"missing start rejects entire snapshot", `{"id":1}`, `[{"fingerprint":"keep","status":{"state":"active"},"labels":{"alertname":"Demo"}}]`, true, true},
		{"invalid start rejects entire snapshot", `{"id":1}`, strings.Replace("["+alert+"]", "2026-11-09T00:00:00.123Z", "invalid", 1), true, true},
		{"missing state rejects entire snapshot", `{"id":1}`, strings.Replace("["+alert+"]", `"state":"active"`, `"other":"active"`, 1), true, true},
		{"unknown state rejects entire snapshot", `{"id":1}`, strings.Replace("["+alert+"]", `"state":"active"`, `"state":"unknown"`, 1), true, true},
		{"missing identity labels rejects snapshot", `{"id":1}`, strings.Replace("["+alert+"]", `"alertname":"Demo"`, `"severity":"warning"`, 1), true, true},
		{"trailing delimiter rejects alerts", `{"id":1}`, `[]]`, true, true},
		{"trailing delimiter rejects org", `{"id":1}]`, `[]`, true, true},
		{"oversized org response rejected", `{"id":1}` + strings.Repeat(" ", 70*1024), `[]`, true, true},
		{"returned suppressed alert survives clock skew", `{"id":1}`, `[{"fingerprint":"keep","startsAt":"2026-11-09T00:00:00.123Z","endsAt":"2000-01-01T00:00:00Z","status":{"state":"suppressed"},"labels":{"alertname":"Demo"}}]`, false, true},
		{"valid empty snapshot recovers", `{"id":1}`, `[]`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			err = store.SyncSnapshot(ctx, 1, time.Now(), []storage.SnapshotAlert{{Fingerprint: "keep", StartsAt: "2026-11-09T00:00:00.123Z", Labels: map[string]string{"alertname": "Demo"}}})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if readinessFixture(w, r) {
					return
				}
				if r.URL.Path == "/api/org" {
					_, _ = w.Write([]byte(tc.org))
					return
				}
				_, _ = w.Write([]byte(tc.alerts))
			}))
			defer server.Close()
			err = problemsync.NewSyncer(nil).Sync(ctx, store, 1, server.URL, "fixture-token")
			if (err != nil) != tc.wantError {
				t.Errorf("error = %v, wantError = %v", err, tc.wantError)
			}
			_, total, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			if (total == 1) != tc.wantActive {
				t.Errorf("active = %d, wantActive = %v", total, tc.wantActive)
			}
		})
	}
}

func TestConcurrentRefreshesAreSerialized(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if readinessFixture(w, r) {
			return
		}
		if r.URL.Path == "/api/org" {
			if calls.Add(1) == 1 {
				close(firstEntered)
				<-releaseFirst
			} else {
				secondEntered <- struct{}{}
			}
			_, _ = w.Write([]byte(`{"id":1}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	syncer := problemsync.NewSyncer(nil)
	results := make(chan error, 2)
	go func() { results <- syncer.Sync(ctx, store, 1, server.URL, "fixture-token") }()
	<-firstEntered
	go func() { results <- syncer.Sync(ctx, store, 1, server.URL, "fixture-token") }()
	overlapped := false
	select {
	case <-secondEntered:
		overlapped = true
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if overlapped {
		t.Fatal("a second snapshot request overlapped a blocked refresh")
	}
}
