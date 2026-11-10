package sync_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
	problemsync "github.com/mlimarenko/grafana-problem-desk/pkg/sync"
)

func TestRuntimeInstanceStateReasons(t *testing.T) {
	cases := []struct {
		name, state, ruleState, delivery string
		wantError, wantActive            bool
	}{
		{"native normal no data", "Normal (NoData)", "inactive", "", false, false},
		{"normal error", "Normal (Error)", "inactive", "", false, false},
		{"normal missing series", "Normal (MissingSeries)", "inactive", "", false, false},
		{"normal combined reason", "Normal (NoData, KeepLast)", "inactive", "", false, false},
		{"normal future reason", "Normal (Future reason)", "inactive", "", false, false},
		{"normal nested reason", "Normal (Future (detail))", "inactive", "", false, false},
		{"pending future reason", "Pending (FutureReason)", "pending", "", false, false},
		{"pending keep last", "Pending (Error, KeepLast)", "pending", "", false, false},
		{"alerting no data", "Alerting (NoData)", "firing", "normal", false, true},
		{"alerting error", "Alerting (Error)", "firing", "normal", false, true},
		{"alerting keep last", "Alerting (Error, KeepLast)", "firing", "normal", false, true},
		{"recovering reason", "Recovering (MissingSeries)", "firing", "normal", false, true},
		{"no data base reason", "NoData (FutureReason)", "inactive", "nodata", false, true},
		{"error base reason", "Error (FutureReason)", "inactive", "error", false, true},
		{"missing delivered alert", "Alerting (NoData)", "firing", "", true, true},
		{"wrong delivery rule", "Alerting (NoData)", "firing", "wrong-rule", true, true},
		{"wrong delivery start", "Alerting (NoData)", "firing", "wrong-start", true, true},
		{"wrong delivery labels", "Alerting (NoData)", "firing", "wrong-label", true, true},
		{"unknown base", "Unknown (NoData)", "inactive", "", true, true},
		{"unknown base bare", "Mystery", "inactive", "", true, true},
		{"missing reason", "Normal ()", "inactive", "", true, true},
		{"blank reason", "Normal ( )", "inactive", "", true, true},
		{"unterminated reason", "Normal (NoData", "inactive", "", true, true},
		{"trailing garbage", "Normal (NoData)garbage", "inactive", "", true, true},
		{"extra closing parenthesis", "Normal (NoData))", "inactive", "", true, true},
		{"newline reason", "Normal (No\nData)", "inactive", "", true, true},
		{"long informational reason", "Normal (" + strings.Repeat("x", 2048) + ")", "inactive", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if err := store.SyncSnapshot(ctx, 1, time.Now(), []storage.SnapshotAlert{{Fingerprint: "keep", StartsAt: "2026-11-09T00:00:00Z", Labels: map[string]string{"alertname": "Demo", "__alert_rule_uid__": "rule"}}}); err != nil {
				t.Fatal(err)
			}
			before, _, err := getProblems(store, ctx, 1, "active", "", "", 10, 0)
			if err != nil {
				t.Fatal(err)
			}
			id := before[0].ID
			if _, err := store.AcknowledgeProblem(ctx, 1, id, "operator", "note"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AssignProblem(ctx, 1, id, "operator", "owner"); err != nil {
				t.Fatal(err)
			}
			stateJSON, _ := json.Marshal(tc.state)
			runtime := fmt.Sprintf(`{"status":"success","data":{"groups":[{"rules":[{"uid":"rule","state":%q,"isPaused":false,"lastEvaluation":"2026-11-09T00:00:01Z","alerts":[{"state":%s,"activeAt":"2026-11-09T00:00:00Z","labels":{"alertname":"Demo"}}]}]}]}}`, tc.ruleState, stateJSON)
			alerts := `[]`
			if tc.delivery != "" {
				name, extra := "Demo", ""
				if tc.delivery == "nodata" {
					name, extra = "DatasourceNoData", `,"rulename":"Demo"`
				}
				if tc.delivery == "error" {
					name, extra = "DatasourceError", `,"rulename":"Demo"`
				}
				alerts = fmt.Sprintf(`[{"fingerprint":"keep","startsAt":"2026-11-09T00:00:00Z","status":{"state":"active"},"labels":{"__alert_rule_uid__":"rule","alertname":%q%s}}]`, name, extra)
				switch tc.delivery {
				case "wrong-rule":
					alerts = strings.Replace(alerts, `"__alert_rule_uid__":"rule"`, `"__alert_rule_uid__":"other"`, 1)
				case "wrong-start":
					alerts = strings.Replace(alerts, `2026-11-09T00:00:00Z`, `2026-11-09T00:00:01Z`, 1)
				case "wrong-label":
					alerts = strings.Replace(alerts, `"alertname":"Demo"`, `"alertname":"Other"`, 1)
				}
			}
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
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			syncer := problemsync.NewSyncer(nil)
			err = syncer.Sync(ctx, store, 1, server.URL, "token")
			if (err != nil) != tc.wantError {
				t.Fatalf("sync err=%v, wantError=%t", err, tc.wantError)
			}
			if !tc.wantError {
				if err := syncer.Sync(ctx, store, 1, server.URL, "token"); err != nil {
					t.Fatal(err)
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
			recovered := 0
			for _, e := range events {
				if e.Kind == "recovered" {
					recovered++
				}
			}
			wantRecovered := 0
			if !tc.wantActive {
				wantRecovered = 1
			}
			if (p.Status == "active") != tc.wantActive || recovered != wantRecovered || p.Fingerprint != "keep" || p.AcknowledgedBy != "operator" || p.Assignee != "owner" || len(comments) != 1 {
				t.Fatalf("episode=%#v recovered=%d comments=%#v", p, recovered, comments)
			}
		})
	}
}
