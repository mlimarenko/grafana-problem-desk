package sync_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
	problemsync "github.com/mlimarenko/grafana-problem-desk/pkg/sync"
)

func TestReadinessProtectsExistingEpisode(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SyncSnapshot(ctx, 1, time.Now(), []storage.SnapshotAlert{{Fingerprint: "keep", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo", "__alert_rule_uid__": "rule"}}}); err != nil {
		t.Fatal(err)
	}
	problems, _, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	id := problems[0].ID
	if _, err := store.AcknowledgeProblem(ctx, 1, id, "operator", "note"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutAssignees(ctx, 1, "admin", 0, []models.AssigneeMember{{UID: "owner", Login: "owner"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignProblem(ctx, 1, id, "operator", "owner"); err != nil {
		t.Fatal(err)
	}
	inventory := `{"folder":[{"name":"group","rules":[{"grafana_alert":{"uid":"rule","is_paused":false,"updated":"2026-11-09T00:00:00Z"}}]}]}`
	runtime := `{"status":"success","data":{"groups":[]}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/org":
			_, _ = w.Write([]byte(`{"id":1}`))
		case "/api/ruler/grafana/api/v1/rules":
			_, _ = w.Write([]byte(inventory))
		case "/api/prometheus/grafana/api/v1/rules":
			_, _ = w.Write([]byte(runtime))
		case "/api/alertmanager/grafana/api/v2/alerts":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := problemsync.NewSyncer(nil)
	if err := s.Sync(ctx, store, 1, server.URL, "token"); err == nil {
		t.Error("unloaded configured rule must reject snapshot")
	}
	p, err := store.GetProblemByID(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.GetEvents(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	comments, err := store.GetComments(ctx, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "active" || p.AcknowledgedBy != "operator" || p.Assignee != "owner" || len(events) != 2 || len(comments) != 1 {
		t.Fatalf("warmup damaged episode: %#v events=%#v comments=%#v", p, events, comments)
	}

	runtime = `{"status":"success","data":{"groups":[{"rules":[{"uid":"rule","state":"firing","isPaused":false,"lastEvaluation":"2026-11-09T00:00:01Z","alerts":[{"state":"Alerting","activeAt":"2026-11-09T00:00:00Z","labels":{"alertname":"Demo"}}]}]}]}}`
	if err := s.Sync(ctx, store, 1, server.URL, "token"); err == nil {
		t.Error("undelivered firing alert must reject snapshot")
	}
	p, _ = store.GetProblemByID(ctx, 1, id)
	if p.Status != "active" {
		t.Fatal("undelivered firing alert recovered")
	}

	runtime = `{"status":"success","data":{"groups":[{"rules":[{"uid":"rule","state":"inactive","isPaused":false,"lastEvaluation":"2026-11-09T00:00:01Z","alerts":[]}]}]}}`
	for range 2 {
		if err := s.Sync(ctx, store, 1, server.URL, "token"); err != nil {
			t.Fatal(err)
		}
	}
	p, _ = store.GetProblemByID(ctx, 1, id)
	events, _ = store.GetEvents(ctx, 1, id)
	if p.Status != "recovered" || len(events) != 3 || p.Assignee != "owner" || p.AcknowledgedBy != "operator" {
		t.Fatalf("complete snapshot: %#v events=%#v", p, events)
	}
}

func TestReadinessSyntheticDatasourceAlerts(t *testing.T) {
	for _, tc := range []struct{ state, alertname string }{{"NoData", "DatasourceNoData"}, {"Error", "DatasourceError"}} {
		t.Run(tc.state, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.SyncSnapshot(ctx, 1, time.Now(), []storage.SnapshotAlert{{Fingerprint: "keep", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo"}}}); err != nil {
				t.Fatal(err)
			}
			original, _, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			id := original[0].ID
			if _, err := store.AcknowledgeProblem(ctx, 1, id, "operator", "note"); err != nil {
				t.Fatal(err)
			}
			alerts := `[]`
			runtime := fmt.Sprintf(`{"status":"success","data":{"groups":[{"rules":[{"uid":"rule","name":"Demo","state":"inactive","isPaused":false,"lastEvaluation":"2026-11-09T00:00:01Z","alerts":[{"state":%q,"activeAt":"2026-11-09T00:00:00Z","labels":{"alertname":"Demo","datasource_uid":"source","grafana_folder":"folder","ref_id":"A"}}]}]}]}}`, tc.state)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/org":
					_, _ = w.Write([]byte(`{"id":1}`))
				case "/api/ruler/grafana/api/v1/rules":
					_, _ = w.Write([]byte(`{"folder":[{"name":"group","rules":[{"grafana_alert":{"uid":"rule","is_paused":false,"updated":"2026-11-09T00:00:00Z"}}]}]}`))
				case "/api/prometheus/grafana/api/v1/rules":
					_, _ = w.Write([]byte(runtime))
				case "/api/alertmanager/grafana/api/v2/alerts":
					_, _ = w.Write([]byte(alerts))
				}
			}))
			defer server.Close()
			s := problemsync.NewSyncer(nil)
			if err := s.Sync(ctx, store, 1, server.URL, "token"); err == nil {
				t.Fatal("missing synthetic alert accepted")
			}
			_, total, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
			if err != nil || total != 1 {
				t.Fatalf("existing episode lost: total=%d err=%v", total, err)
			}
			correct := fmt.Sprintf(`[{"fingerprint":"synthetic","startsAt":"2026-11-09T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":%q,"rulename":"Demo","datasource_uid":"source","grafana_folder":"folder","ref_id":"A"}}]`, tc.alertname)
			for _, mismatch := range []string{
				strings.Replace(correct, `"__alert_rule_uid__":"rule"`, `"__alert_rule_uid__":"other"`, 1),
				strings.Replace(correct, `"rulename":"Demo"`, `"rulename":"Other"`, 1),
				strings.Replace(correct, `"datasource_uid":"source"`, `"datasource_uid":"other"`, 1),
				strings.Replace(correct, `2026-11-09T00:00:00Z`, `2026-11-09T00:00:01Z`, 1),
			} {
				alerts = mismatch
				if err := s.Sync(ctx, store, 1, server.URL, "token"); err == nil {
					t.Fatal("mismatched synthetic identity accepted")
				}
			}
			p, err := store.GetProblemByID(ctx, 1, id)
			if err != nil {
				t.Fatal(err)
			}
			events, err := store.GetEvents(ctx, 1, id)
			if err != nil {
				t.Fatal(err)
			}
			comments, err := store.GetComments(ctx, 1, id)
			if err != nil {
				t.Fatal(err)
			}
			if p.Status != "active" || p.AcknowledgedBy != "operator" || len(events) != 1 || len(comments) != 1 {
				t.Fatalf("uncertain state damaged human data: %#v events=%#v comments=%#v", p, events, comments)
			}
			alerts = correct
			if err := s.Sync(ctx, store, 1, server.URL, "token"); err != nil {
				t.Fatalf("delivered synthetic alert rejected: %v", err)
			}
			problems, total, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
			if err != nil || total != 1 || problems[0].Fingerprint != "synthetic" {
				t.Fatalf("synthetic episode missing: %#v total=%d err=%v", problems, total, err)
			}
		})
	}
}

func TestReadinessGrafanaEmptyLabels(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SyncSnapshot(ctx, 1, time.Now(), []storage.SnapshotAlert{{Fingerprint: "prior", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Prior"}}}); err != nil {
		t.Fatal(err)
	}
	alerts := `[{"fingerprint":"native-fingerprint","startsAt":"2026-11-09T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":"Demo","env":""}}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/org":
			_, _ = w.Write([]byte(`{"id":1}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			_, _ = w.Write([]byte(alerts))
		case "/api/ruler/grafana/api/v1/rules":
			_, _ = w.Write([]byte(`{"folder":[{"name":"group","rules":[{"grafana_alert":{"uid":"rule","is_paused":false,"updated":"2026-11-09T00:00:00Z"}}]}]}`))
		case "/api/prometheus/grafana/api/v1/rules":
			_, _ = w.Write([]byte(`{"status":"success","data":{"groups":[{"rules":[{"uid":"rule","state":"firing","isPaused":false,"lastEvaluation":"2026-11-09T00:00:01Z","alerts":[{"state":"Alerting","activeAt":"2026-11-09T00:00:00Z","labels":{"alertname":"Demo","env":""}}]}]}]}}`))
		}
	}))
	defer server.Close()
	s := problemsync.NewSyncer(nil)
	if err := s.Sync(ctx, store, 1, server.URL, "token"); err == nil {
		t.Fatal("unexpected Alertmanager empty label must not match silently")
	}
	_, total, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
	if err != nil || total != 1 {
		t.Fatalf("uncertain snapshot recovered prior episode: %d %v", total, err)
	}
	alerts = strings.Replace(alerts, `,"env":""`, "", 1)
	if err := s.Sync(ctx, store, 1, server.URL, "token"); err != nil {
		t.Fatal(err)
	}
	problems, total, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
	if err != nil || total != 1 || problems[0].Fingerprint != "native-fingerprint" {
		t.Fatalf("fingerprint changed: %#v total=%d err=%v", problems, total, err)
	}
	if _, ok := problems[0].Labels["env"]; ok {
		t.Fatalf("invented empty label: %#v", problems[0].Labels)
	}
}

func TestMissingAlertmanagerNeverRecovers(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SyncSnapshot(ctx, 2, time.Now(), []storage.SnapshotAlert{{Fingerprint: "keep", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo"}}}); err != nil {
		t.Fatal(err)
	}
	problems, _, err := getProblems(store, ctx, 2, "active", "", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	id := problems[0].ID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/org" {
			_, _ = w.Write([]byte(`{"id":2}`))
			return
		}
		if r.URL.Path == "/api/alertmanager/grafana/api/v2/alerts" {
			http.Error(w, `{"message":"Alertmanager does not exist for this organization"}`, http.StatusNotFound)
			return
		}
		t.Errorf("unexpected source request: %s", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()
	if err := problemsync.NewSyncer(nil).Sync(ctx, store, 2, server.URL, "token"); err == nil {
		t.Fatal("uninitialized Alertmanager accepted")
	}
	p, err := store.GetProblemByID(ctx, 2, id)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.GetEvents(ctx, 2, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "active" || len(events) != 0 {
		t.Fatalf("404 changed episode: %#v events=%#v", p, events)
	}
}

func TestReadinessHTTPFailsClosed(t *testing.T) {
	for _, path := range []string{"/api/ruler/grafana/api/v1/rules", "/api/prometheus/grafana/api/v1/rules"} {
		for _, code := range []int{http.StatusForbidden, http.StatusFound} {
			t.Run(path+http.StatusText(code), func(t *testing.T) {
				ctx := context.Background()
				store, err := storage.Open(":memory:")
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				if err := store.SyncSnapshot(ctx, 1, time.Now(), []storage.SnapshotAlert{{Fingerprint: "keep", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo"}}}); err != nil {
					t.Fatal(err)
				}
				var leaked atomic.Bool
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
				defer target.Close()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == path {
						if code == http.StatusFound {
							http.Redirect(w, r, target.URL, code)
						} else {
							http.Error(w, "denied", code)
						}
						return
					}
					switch r.URL.Path {
					case "/api/org":
						_, _ = w.Write([]byte(`{"id":1}`))
					case "/api/alertmanager/grafana/api/v2/alerts":
						_, _ = w.Write([]byte(`[]`))
					case "/api/ruler/grafana/api/v1/rules":
						_, _ = w.Write([]byte(`{}`))
					case "/api/prometheus/grafana/api/v1/rules":
						_, _ = w.Write([]byte(`{"status":"success","data":{"groups":[]}}`))
					}
				}))
				defer server.Close()
				err = problemsync.NewSyncer(&http.Client{}).Sync(ctx, store, 1, server.URL, "token")
				if err == nil || leaked.Load() {
					t.Fatalf("error=%v redirected=%v", err, leaked.Load())
				}
				_, total, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
				if err != nil || total != 1 {
					t.Fatalf("active=%d err=%v", total, err)
				}
			})
		}
	}
}

func TestReadinessEmptyAndFailureCases(t *testing.T) {
	const active = `{"folder":[{"name":"group","rules":[{"grafana_alert":{"uid":"rule","is_paused":false,"updated":"2026-11-09T00:00:00Z"}}]}]}`
	const paused = `{"folder":[{"name":"group","rules":[{"grafana_alert":{"uid":"rule","is_paused":true,"updated":"2026-11-09T00:00:00Z"}}]}]}`
	const emptyRuntime = `{"status":"success","data":{"groups":[]}}`
	const pending = `{"status":"success","data":{"groups":[{"rules":[{"uid":"rule","state":"pending","isPaused":false,"lastEvaluation":"2026-11-09T00:00:01Z","alerts":[{"state":"Pending","labels":{"alertname":"Demo"}}]}]}]}}`
	const firing = `{"status":"success","data":{"groups":[{"rules":[{"uid":"rule","state":"firing","isPaused":false,"lastEvaluation":"2026-11-09T00:00:01Z","alerts":[{"state":"Alerting","activeAt":"2026-11-09T00:00:00Z","labels":{"alertname":"Demo","host":"a"}},{"state":"Alerting","activeAt":"2026-11-09T00:00:00Z","labels":{"alertname":"Demo","host":"b"}}]}]}]}}`
	cases := []struct {
		name, inventory, runtime, alerts string
		wantError, active                bool
	}{
		{"all rules deleted", `{}`, emptyRuntime, `[]`, false, false},
		{"paused rule", paused, emptyRuntime, `[]`, false, false},
		{"pending is not firing", active, pending, `[]`, false, false},
		{"pending no data is not firing", active, strings.Replace(pending, `"state":"Pending"`, `"state":"Pending (NoData)"`, 1), `[]`, false, false},
		{"pending error is not firing", active, strings.Replace(pending, `"state":"Pending"`, `"state":"Pending (Error)"`, 1), `[]`, false, false},
		{"missing evaluation", active, strings.Replace(pending, "2026-11-09T00:00:01Z", "0001-01-01T00:00:00Z", 1), `[]`, true, true},
		{"missing instances field", active, strings.Replace(pending, `,"alerts":[{"state":"Pending","labels":{"alertname":"Demo"}}]`, "", 1), `[]`, true, true},
		{"pending without pending instance", active, strings.Replace(pending, `"state":"Pending"`, `"state":"Normal"`, 1), `[]`, true, true},
		{"firing with only pending instance", active, strings.Replace(pending, `"state":"pending"`, `"state":"firing"`, 1), `[]`, true, true},
		{"stale evaluation", active, strings.Replace(pending, "2026-11-09T00:00:01Z", "2026-11-08T00:00:00Z", 1), `[]`, true, true},
		{"partial firing delivery", active, firing, `[{"fingerprint":"a","startsAt":"2026-11-09T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":"Demo","host":"a"}}]`, true, true},
		{"both firing delivered", active, firing, `[{"fingerprint":"a","startsAt":"2026-11-09T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":"Demo","host":"a"}},{"fingerprint":"b","startsAt":"2026-11-09T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":"Demo","host":"b"}}]`, false, false},
		{"old occurrence is not current delivery", active, firing, `[{"fingerprint":"a","startsAt":"2026-11-08T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":"Demo","host":"a"}},{"fingerprint":"b","startsAt":"2026-11-09T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":"Demo","host":"b"}}]`, true, true},
		{"bad inventory", `{"folder":null}`, emptyRuntime, `[]`, true, true},
		{"missing pause", `{"folder":[{"name":"group","rules":[{"grafana_alert":{"uid":"rule"}}]}]}`, emptyRuntime, `[]`, true, true},
		{"null runtime", active, `null`, `[]`, true, true},
		{"missing groups", active, `{"status":"success","data":{}}`, `[]`, true, true},
		{"trailing runtime", active, emptyRuntime + `]`, `[]`, true, true},
		{"oversized inventory", `{}` + strings.Repeat(" ", 16*1024*1024), emptyRuntime, `[]`, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.SyncSnapshot(ctx, 1, time.Now(), []storage.SnapshotAlert{{Fingerprint: "keep", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo"}}}); err != nil {
				t.Fatal(err)
			}
			problems, _, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			id := problems[0].ID
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/org":
					_, _ = w.Write([]byte(`{"id":1}`))
				case "/api/ruler/grafana/api/v1/rules":
					_, _ = w.Write([]byte(tc.inventory))
				case "/api/prometheus/grafana/api/v1/rules":
					_, _ = w.Write([]byte(tc.runtime))
				case "/api/alertmanager/grafana/api/v2/alerts":
					_, _ = w.Write([]byte(tc.alerts))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			err = problemsync.NewSyncer(nil).Sync(ctx, store, 1, server.URL, "token")
			if (err != nil) != tc.wantError {
				t.Fatalf("sync error=%v, wantError=%v", err, tc.wantError)
			}
			p, err := store.GetProblemByID(ctx, 1, id)
			if err != nil {
				t.Fatal(err)
			}
			if (p.Status == "active") != tc.active {
				t.Fatalf("episode status %s", p.Status)
			}
			events, err := store.GetEvents(ctx, 1, id)
			if err != nil {
				t.Fatal(err)
			}
			if tc.active && len(events) != 0 {
				t.Fatalf("unexpected events: %#v", events)
			}
		})
	}
}
