package sync_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
	problemsync "github.com/mlimarenko/grafana-problem-desk/pkg/sync"
)

func getProblems(s *storage.Store, ctx context.Context, org int64, state, severity, search string, limit, offset int) ([]models.Problem, int, error) {
	now := time.Now().UTC()
	r, err := s.GetProblems(ctx, org, storage.ProblemQuery{View: "history", State: state, Severity: severity, Search: search, Sort: "started", Order: "desc", From: now.AddDate(-200, 0, 0), To: now.AddDate(200, 0, 0), Now: now, Limit: limit, Offset: offset})
	return r.Problems, r.Total, err
}

func readinessFixture(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/ruler/grafana/api/v1/rules", "/custom-grafana/api/ruler/grafana/api/v1/rules":
		_, _ = w.Write([]byte(`{}`))
		return true
	case "/api/prometheus/grafana/api/v1/rules", "/custom-grafana/api/prometheus/grafana/api/v1/rules":
		_, _ = w.Write([]byte(`{"status":"success","data":{"groups":[]}}`))
		return true
	}
	return false
}

func TestSyncerSuccessAndSnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	trustedOrgID := int64(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer secret-token-123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if readinessFixture(w, r) {
			return
		}

		switch r.URL.Path {
		case "/api/org":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 1, "name": "Main Org"}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[
				{
					"fingerprint": "fp-1",
					"startsAt": "2026-11-09T10:00:00Z",
					"status": {"state": "active"},
					"labels": {"alertname": "CPUHigh", "severity": "critical"},
					"annotations": {"summary": "CPU > 90%"},
					"generatorURL": "http://prometheus/graph"
				}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	syncer := problemsync.NewSyncer(nil)
	if err := syncer.Sync(ctx, store, trustedOrgID, server.URL, "secret-token-123"); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	probs, total, err := getProblems(store, ctx, trustedOrgID, "active", "", "", 10, 0)
	if err != nil || total != 1 {
		t.Fatalf("expected 1 active problem, got %d, err: %v", total, err)
	}
	if probs[0].Fingerprint != "fp-1" {
		t.Errorf("expected fingerprint fp-1, got %s", probs[0].Fingerprint)
	}

	syncStatus, err := store.GetSyncStatus(ctx, trustedOrgID)
	if err != nil || syncStatus.LastSuccess == "" || syncStatus.Error != "" {
		t.Errorf("expected successful sync status, got %+v", syncStatus)
	}
}

func TestSyncerOrgMismatchNeverResolves(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	trustedOrgID := int64(1)
	t0 := time.Now().UTC()

	// Pre-seed an active problem in DB
	initialAlert := storage.SnapshotAlert{
		Fingerprint:  "fp-keep-active",
		StartsAt:     t0.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "Important"},
		Annotations:  map[string]string{"summary": "Should not recover on source failure"},
		GeneratorURL: "http://prometheus",
	}
	if err := store.SyncSnapshot(ctx, trustedOrgID, t0, []storage.SnapshotAlert{initialAlert}); err != nil {
		t.Fatalf("initial seed failed: %v", err)
	}

	// Server returns mismatching org id: 999 instead of 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/org" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 999}`))
			return
		}
		// Empty alerts (if called, which should NOT happen or resolve)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	syncer := problemsync.NewSyncer(nil)
	err = syncer.Sync(ctx, store, trustedOrgID, server.URL, "token")
	if err == nil {
		t.Fatalf("expected error on org mismatch, got nil")
	}

	// Verify problem is STILL active (NO false recovery!)
	probs, total, err := getProblems(store, ctx, trustedOrgID, "active", "", "", 10, 0)
	if err != nil || total != 1 || len(probs) != 1 {
		t.Fatalf("expected problem to remain active after org mismatch, got total: %d", total)
	}
	if probs[0].Status != "active" {
		t.Errorf("problem status changed to %s on failed sync", probs[0].Status)
	}

	syncStatus, err := store.GetSyncStatus(ctx, trustedOrgID)
	if err != nil || syncStatus.Error == "" {
		t.Errorf("expected sync status error recorded, got %+v", syncStatus)
	}
}

func TestSyncerFailedSourceNeverResolves(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	trustedOrgID := int64(1)
	t0 := time.Now().UTC()

	initialAlert := storage.SnapshotAlert{
		Fingerprint:  "fp-keep-active",
		StartsAt:     t0.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "CriticalDB"},
		Annotations:  map[string]string{"summary": "Master down"},
		GeneratorURL: "http://prometheus",
	}
	_ = store.SyncSnapshot(ctx, trustedOrgID, t0, []storage.SnapshotAlert{initialAlert})

	// Server fails with 500 Internal Server Error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	syncer := problemsync.NewSyncer(nil)
	err = syncer.Sync(ctx, store, trustedOrgID, server.URL, "token")
	if err == nil {
		t.Fatalf("expected error on 500 response, got nil")
	}

	probs, total, _ := getProblems(store, ctx, trustedOrgID, "active", "", "", 10, 0)
	if total != 1 || probs[0].Status != "active" {
		t.Fatalf("active problem was wrongly marked recovered on 500 error!")
	}
}

func TestSyncerTruncatedOrMalformedJSONNeverResolves(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	trustedOrgID := int64(1)
	t0 := time.Now().UTC()

	initialAlert := storage.SnapshotAlert{
		Fingerprint:  "fp-keep-active",
		StartsAt:     t0.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "ServiceDown"},
		Annotations:  map[string]string{"summary": "Critical alert"},
		GeneratorURL: "http://prometheus",
	}
	_ = store.SyncSnapshot(ctx, trustedOrgID, t0, []storage.SnapshotAlert{initialAlert})

	testCases := []struct {
		name string
		body string
	}{
		{
			name: "truncated midway JSON",
			body: `[{"fingerprint": "fp-1", "startsAt": "2026-11-09T10:00:00Z", "status": {"state": "active"},`,
		},
		{
			name: "malformed JSON syntax",
			body: `[not a json array]`,
		},
		{
			name: "trailing data after array",
			body: `[] extra-data-after-json`,
		},
		{
			name: "trailing delimiter bracket",
			body: `[]]`,
		},
		{
			name: "null JSON body",
			body: `null`,
		},
		{
			name: "missing startsAt",
			body: `[{"fingerprint": "fp-nostarts", "status": {"state": "active"}, "labels": {"alertname": "A"}}]`,
		},
		{
			name: "invalid startsAt",
			body: `[{"fingerprint": "fp-badstarts", "startsAt": "not-a-timestamp", "status": {"state": "active"}, "labels": {"alertname": "A"}}]`,
		},
		{
			name: "missing status",
			body: `[{"fingerprint": "fp-nostat", "startsAt": "2026-11-09T10:00:00Z", "labels": {"alertname": "A"}}]`,
		},
		{
			name: "unknown status state",
			body: `[{"fingerprint": "fp-badstat", "startsAt": "2026-11-09T10:00:00Z", "status": {"state": "invalid_state"}, "labels": {"alertname": "A"}}]`,
		},
		{
			name: "missing alertname label",
			body: `[{"fingerprint": "fp-noname", "startsAt": "2026-11-09T10:00:00Z", "status": {"state": "active"}, "labels": {"env": "prod"}}]`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/org" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"id": 1}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.body))
			}))
			defer server.Close()

			syncer := problemsync.NewSyncer(nil)
			err := syncer.Sync(ctx, store, trustedOrgID, server.URL, "token")
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}

			// Must NOT cause false recoveries
			probs, total, _ := getProblems(store, ctx, trustedOrgID, "active", "", "", 10, 0)
			if total != 1 || probs[0].Status != "active" {
				t.Fatalf("false recovery occurred for test case %s!", tc.name)
			}
		})
	}
}

func TestSyncerSuppressedPastEndsAtPreserved(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	trustedOrgID := int64(1)
	pastEndsAt := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if readinessFixture(w, r) {
			return
		}
		if r.URL.Path == "/api/org" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 1}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Alert is currently suppressed and has past endsAt in source response
		w.Write([]byte(fmt.Sprintf(`[
			{
				"fingerprint": "fp-suppressed-past",
				"startsAt": "2026-11-09T08:00:00Z",
				"endsAt": %q,
				"status": {"state": "suppressed"},
				"labels": {"alertname": "SilencedIncident"}
			}
		]`, pastEndsAt)))
	}))
	defer server.Close()

	syncer := problemsync.NewSyncer(nil)
	if err := syncer.Sync(ctx, store, trustedOrgID, server.URL, "token"); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// Must be kept active (not falsely skipped / recovered)
	probs, total, err := getProblems(store, ctx, trustedOrgID, "active", "", "", 10, 0)
	if err != nil || total != 1 || len(probs) != 1 {
		t.Fatalf("expected suppressed alert with past endsAt to be stored as active, got total %d", total)
	}
	if probs[0].Fingerprint != "fp-suppressed-past" {
		t.Errorf("expected fp-suppressed-past, got %s", probs[0].Fingerprint)
	}
}

func TestSyncerTokenNotRedirected(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	trustedOrgID := int64(1)

	var redirectTargetReached atomic.Bool
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetReached.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	// Initial server sends 302 redirect to redirectTarget
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/api/org", http.StatusFound)
	}))
	defer sourceServer.Close()

	syncer := problemsync.NewSyncer(nil)
	err = syncer.Sync(ctx, store, trustedOrgID, sourceServer.URL, "my-super-secret-bearer-token")
	if err == nil {
		t.Fatalf("expected error on redirect, got nil")
	}

	if redirectTargetReached.Load() {
		t.Fatalf("bearer token client followed redirect to external server! Security violation.")
	}
}

func TestSyncerFallbackFingerprint(t *testing.T) {
	labels := map[string]string{
		"env":       "prod",
		"alertname": "MemHigh",
		"service":   "billing",
	}

	fp1 := problemsync.ComputeFallbackFingerprint(labels)
	fp2 := problemsync.ComputeFallbackFingerprint(labels)

	if fp1 == "" || fp1 != fp2 {
		t.Fatalf("fallback fingerprint is not deterministic: %q vs %q", fp1, fp2)
	}

	// Change label value: fingerprint must change
	labels2 := map[string]string{
		"env":       "staging",
		"alertname": "MemHigh",
		"service":   "billing",
	}
	fp3 := problemsync.ComputeFallbackFingerprint(labels2)
	if fp1 == fp3 {
		t.Fatalf("different labels produced same fingerprint")
	}
}

func TestSyncerURLValidation(t *testing.T) {
	invalidURLs := []string{
		"",
		"ftp://grafana.example.com",
		"http://user:pass@grafana.example.com",
		"http://grafana.example.com?query=val",
		"http://grafana.example.com#fragment",
	}

	for _, raw := range invalidURLs {
		if _, err := problemsync.ValidateSourceURL(raw); err == nil {
			t.Errorf("expected URL validation failure for %q, got nil", raw)
		}
	}

	validURLs := []string{
		"http://grafana.example.com",
		"https://grafana.example.com:3000",
		"http://127.0.0.1:3000/subpath",
	}

	for _, raw := range validURLs {
		if _, err := problemsync.ValidateSourceURL(raw); err != nil {
			t.Errorf("expected URL validation success for %q, got %v", raw, err)
		}
	}
}

func TestNormalizeTimestamp_PreservesFractionalSeconds(t *testing.T) {
	ts1 := "2026-11-09T10:00:00.123456Z"
	ts2 := "2026-11-09T10:00:00.654321Z"

	norm1, err := problemsync.NormalizeTimestamp(ts1)
	if err != nil {
		t.Fatalf("NormalizeTimestamp(ts1) failed: %v", err)
	}
	norm2, err := problemsync.NormalizeTimestamp(ts2)
	if err != nil {
		t.Fatalf("NormalizeTimestamp(ts2) failed: %v", err)
	}

	if norm1 == norm2 {
		t.Fatalf("timestamps within same second collided: %s == %s", norm1, norm2)
	}
	if norm1 != "2026-11-09T10:00:00.123456Z" {
		t.Errorf("expected fractional precision preserved, got %s", norm1)
	}
}

func TestNormalizeTimestamp_UTCYearBounds(t *testing.T) {
	for _, raw := range []string{"9999-12-31T23:59:59-01:00", "0000-01-01T00:00:00+01:00"} {
		if _, err := problemsync.NormalizeTimestamp(raw); err == nil {
			t.Errorf("accepted out-of-range UTC timestamp %q", raw)
		}
	}
	if got, err := problemsync.NormalizeTimestamp("2026-01-01T03:00:00.123456789+03:00"); err != nil || got != "2026-01-01T00:00:00.123456789Z" {
		t.Fatalf("valid offset: %q, %v", got, err)
	}
}

func TestComputeFallbackFingerprint_DelimiterAmbiguity(t *testing.T) {
	// Two maps that would collide under key=value\n delimiter concatenation
	mapA := map[string]string{
		"k1": "val1\nk2=val2",
	}
	mapB := map[string]string{
		"k1": "val1",
		"k2": "val2",
	}

	fpA := problemsync.ComputeFallbackFingerprint(mapA)
	fpB := problemsync.ComputeFallbackFingerprint(mapB)

	if fpA == fpB {
		t.Fatalf("delimiter ambiguity collision: mapA and mapB produced identical fingerprint %s", fpA)
	}
}

func TestSyncerSubpathPreserved(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}
	defer store.Close()

	trustedOrgID := int64(1)
	var orgPathCalled, alertsPathCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if readinessFixture(w, r) {
			return
		}
		switch r.URL.Path {
		case "/custom-grafana/api/org":
			orgPathCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 1, "name": "Subpath Org"}`))
		case "/custom-grafana/api/alertmanager/grafana/api/v2/alerts":
			alertsPathCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	subpathURL := server.URL + "/custom-grafana/"
	syncer := problemsync.NewSyncer(nil)
	if err := syncer.Sync(ctx, store, trustedOrgID, subpathURL, "token"); err != nil {
		t.Fatalf("sync with subpath failed: %v", err)
	}

	if !orgPathCalled {
		t.Errorf("expected /custom-grafana/api/org to be called")
	}
	if !alertsPathCalled {
		t.Errorf("expected /custom-grafana/api/alertmanager/grafana/api/v2/alerts to be called")
	}
}
