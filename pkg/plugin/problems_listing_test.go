package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/plugin"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

func TestListingViewsFiltersAndIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	alert := func(fp, when, severity, host string) storage.SnapshotAlert {
		return storage.SnapshotAlert{Fingerprint: fp, StartsAt: when, Labels: map[string]string{"severity": severity, "host": host, `hostile."key`: "100%_"}, Annotations: map[string]string{"summary": "Literal 100%_"}}
	}
	old := alert("old", "2025-12-01T00:00:00Z", "warning", "OldHost")
	long := alert("long", "2025-12-01T00:00:00Z", "critical", "CriticalHost")
	delete(long.Labels, "host")
	long.Labels["piping_host_name"] = "CriticalHost"
	long.Labels["instance"] = "192.0.2.1"
	long.Annotations["quote"] = `with "quotes"`
	recent := alert("recent", "2026-01-02T00:00:00.125Z", "warning", "RecentHost")
	active := alert("active", "2026-01-03T00:00:00Z", "warning", "ActiveHost")
	if err = s.SyncSnapshot(ctx, 1, start, []storage.SnapshotAlert{old, long}); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSnapshot(ctx, 1, start.Add(24*time.Hour), []storage.SnapshotAlert{long, recent}); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSnapshot(ctx, 1, start.Add(48*time.Hour), []storage.SnapshotAlert{long, active}); err != nil {
		t.Fatal(err)
	}
	secret := alert("secret", "2026-01-03T00:00:00Z", "critical", "PrivateHost")
	if err = s.SyncSnapshot(ctx, 2, start, []storage.SnapshotAlert{secret}); err != nil {
		t.Fatal(err)
	}
	a := plugin.NewAppWithStore(backend.AppInstanceSettings{}, s, nil)
	defer a.Dispose()
	user := &backend.User{Login: "reader", Role: "Viewer"}
	list := func(query string) models.ProblemsResponse {
		t.Helper()
		response := call(a, http.MethodGet, "problems?"+query, 1, user, nil, nil)
		if response.Status != 200 {
			t.Fatalf("listing status %d body %s", response.Status, response.Body)
		}
		var r models.ProblemsResponse
		if err := json.Unmarshal(response.Body, &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	q := url.Values{"view": {"recent"}, "from": {"2026-01-02T23:59:59Z"}, "to": {"2026-01-04T00:00:00Z"}, "limit": {"1"}}
	r := list(q.Encode())
	if r.Total != 3 || r.Summary.Active != 2 || r.Summary.Recovered != 1 || r.Summary.Critical != 1 || r.Summary.Warning != 2 || len(r.Problems) != 1 || r.Problems[0].Fingerprint != "long" {
		t.Fatalf("recent: %+v", r)
	}
	if strings.Contains(strings.Join(r.Facets.Hosts, ","), "PrivateHost") {
		t.Fatalf("cross-org facet: %+v", r.Facets)
	}
	if !strings.Contains(strings.Join(r.Facets.Hosts, ","), "CriticalHost") {
		t.Fatalf("preferred host missing: %+v", r.Facets)
	}
	longID := r.Problems[0].ID
	q.Set("offset", "1")
	r2 := list(q.Encode())
	if r2.Total != r.Total || r2.Problems[0].ID == r.Problems[0].ID {
		t.Fatalf("pagination: %+v", r2)
	}
	q = url.Values{"sort": {"started"}, "order": {"desc"}}
	if r = list(q.Encode()); len(r.Problems) != 2 || r.Problems[0].Fingerprint != "active" {
		t.Fatalf("started sort: %+v", r)
	}
	q.Set("sort", "duration")
	if r = list(q.Encode()); len(r.Problems) != 2 || r.Problems[0].Fingerprint != "long" {
		t.Fatalf("duration sort: %+v", r)
	}
	q = url.Values{"view": {"history"}, "from": {"2026-01-02T00:00:00Z"}, "to": {"2026-01-02T00:00:00.124Z"}}
	r = list(q.Encode())
	if r.Total != 2 {
		t.Fatalf("history overlap or fractional bound: %+v", r)
	}
	q.Set("to", "2026-01-02T03:00:00.125+03:00")
	r = list(q.Encode())
	if r.Total != 3 {
		t.Fatalf("timezone/fraction: %+v", r)
	}
	q = url.Values{"host": {"critical"}, "search": {"100%_"}, "labelKey": {`hostile."key`}, "labelValue": {"100%_"}, "severity": {"critical"}, "assignment": {"unassigned"}, "ack": {"unacknowledged"}}
	r = list(q.Encode())
	if r.Total != 1 || r.Problems[0].Fingerprint != "long" {
		t.Fatalf("filters: %+v", r)
	}
	q.Set("search", "100X_")
	if r = list(q.Encode()); r.Total != 0 {
		t.Fatalf("literal search: %+v", r)
	}
	q.Set("search", `with "quotes"`)
	if r = list(q.Encode()); r.Total != 1 {
		t.Fatalf("literal quoted search: %+v", r)
	}
	q.Set("labelKey", "' OR 1=1 --")
	if r = list(q.Encode()); r.Total != 0 {
		t.Fatalf("literal label key: %+v", r)
	}
	if _, err = s.AssignProblem(ctx, 1, longID, "operator", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcknowledgeProblem(ctx, 1, longID, "operator", ""); err != nil {
		t.Fatal(err)
	}
	q = url.Values{"assignment": {"assigned"}, "assignee": {"alice"}, "ack": {"acknowledged"}}
	if r = list(q.Encode()); r.Total != 1 || r.Summary.Acknowledged != 1 || r.Summary.Unassigned != 0 || len(r.Facets.Hosts) != 2 {
		t.Fatalf("assigned and acknowledged: %+v", r)
	}
	for _, query := range []string{"view=wat", "state=recovered", "limit=101", "offset=-1", "sort=bad", "from=2026-01-01T00%3A00%3A00Z", "view=history&from=2026-01-01T00%3A00%3A00Z", "view=history&from=2020-01-01T00%3A00%3A00Z&to=2022-01-01T00%3A00%3A00Z", "labelKey=x", "search=%ZZ"} {
		resp := call(a, http.MethodGet, "problems?"+query, 1, user, nil, nil)
		if resp.Status != 400 {
			t.Errorf("%s status %d", query, resp.Status)
		}
	}
}

func TestResolveOccurrence(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := storage.SnapshotAlert{Fingerprint: "same", StartsAt: "2026-01-01T00:00:00.123456789Z", Labels: map[string]string{"key": "value"}}
	second := storage.SnapshotAlert{Fingerprint: "same", StartsAt: "2026-01-02T00:00:00Z", Labels: map[string]string{"key": "value"}}
	if err = s.SyncSnapshot(ctx, 1, t0, []storage.SnapshotAlert{first}); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSnapshot(ctx, 1, t0.Add(24*time.Hour), []storage.SnapshotAlert{second}); err != nil {
		t.Fatal(err)
	}
	app := plugin.NewAppWithStore(backend.AppInstanceSettings{}, s, nil)
	defer app.Dispose()
	user := &backend.User{Login: "reader", Role: "Viewer"}
	resolve := func(q url.Values) *backend.CallResourceResponse {
		return call(app, http.MethodGet, "resolve?"+q.Encode(), 1, user, nil, nil)
	}
	q := url.Values{"fingerprint": {"same"}, "startsAt": {"2026-01-01T03:00:00.123456789+03:00"}}
	r := resolve(q)
	if r.Status != 200 {
		t.Fatalf("resolve status %d: %s", r.Status, r.Body)
	}
	var resolved map[string]string
	if err := json.Unmarshal(r.Body, &resolved); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProblemByID(ctx, 1, resolved["problemId"])
	if err != nil || p.StartsAt != first.StartsAt {
		t.Fatalf("wrong recurrence: %+v %v", p, err)
	}
	q.Set("startsAt", "2026-01-01T00:00:00.123456788Z")
	if r = resolve(q); r.Status != 404 {
		t.Fatalf("unobserved: %d", r.Status)
	}
	q = url.Values{"labels": {`{"key":"value"}`}, "startsAt": {first.StartsAt}}
	if r = resolve(q); r.Status != 200 {
		t.Fatalf("label lookup: %d %s", r.Status, r.Body)
	}
	other := first
	other.Fingerprint = "another"
	if err = s.SyncSnapshot(ctx, 1, t0, []storage.SnapshotAlert{other}); err != nil {
		t.Fatal(err)
	}
	if r = resolve(q); r.Status != 409 {
		t.Fatalf("ambiguous labels: %d", r.Status)
	}
	q.Set("fingerprint", "same")
	if r = resolve(q); r.Status != 400 {
		t.Fatalf("mutually exclusive: %d", r.Status)
	}
	delete(q, "fingerprint")
	q.Set("labels", `{"key":"value","key":"other"}`)
	if r = resolve(q); r.Status != 400 {
		t.Fatalf("duplicate labels: %d", r.Status)
	}
	q.Set("labels", `{"key":null}`)
	if r = resolve(q); r.Status != 400 {
		t.Fatalf("null label: %d", r.Status)
	}
	app2 := plugin.NewAppWithStore(backend.AppInstanceSettings{}, s, nil)
	q = url.Values{"fingerprint": {"same"}, "startsAt": {first.StartsAt}}
	r = call(app2, http.MethodGet, "resolve?"+q.Encode(), 2, user, nil, nil)
	if r.Status != 404 {
		t.Fatalf("cross-org: %d", r.Status)
	}
}

func TestResolveActiveMatchers(t *testing.T) {
	ctx := context.Background()
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	alert := func(fp, device string) storage.SnapshotAlert {
		return storage.SnapshotAlert{Fingerprint: fp, StartsAt: now.Format(time.RFC3339Nano), Labels: map[string]string{"__alert_rule_uid__": "rule", "instance": "host", "device": device}}
	}
	old := alert("old", "recovered")
	a, b := alert("a", "disk-a"), alert("b", "disk-b")
	if err = s.SyncSnapshot(ctx, 1, now, []storage.SnapshotAlert{old}); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSnapshot(ctx, 1, now.Add(time.Minute), []storage.SnapshotAlert{a, b}); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncSnapshot(ctx, 2, now, []storage.SnapshotAlert{alert("secret", "foreign")}); err != nil {
		t.Fatal(err)
	}
	app := plugin.NewAppWithStore(backend.AppInstanceSettings{}, s, nil)
	defer app.Dispose()
	user := &backend.User{Login: "reader", Role: "Viewer"}
	resolve := func(labels string) *backend.CallResourceResponse {
		return call(app, http.MethodGet, "resolve?"+url.Values{"matchers": {labels}}.Encode(), 1, user, nil, nil)
	}
	if r := resolve(`{"__alert_rule_uid__":"rule","instance":"host"}`); r.Status != 409 {
		t.Fatalf("expected ambiguous: %d", r.Status)
	}
	r := resolve(`{"__alert_rule_uid__":"rule","instance":"host","device":"disk-b"}`)
	if r.Status != 200 {
		t.Fatalf("expected unique active: %d %s", r.Status, r.Body)
	}
	var result map[string]string
	if err = json.Unmarshal(r.Body, &result); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProblemByID(ctx, 1, result["problemId"])
	if err != nil || p.Fingerprint != "b" {
		t.Fatalf("wrong active episode: %+v %v", p, err)
	}
	for _, device := range []string{"recovered", "foreign"} {
		r = resolve(`{"__alert_rule_uid__":"rule","instance":"host","device":"` + device + `"}`)
		if r.Status != 404 {
			t.Fatalf("device %s status %d", device, r.Status)
		}
	}
	if r = resolve(`{"__alert_rule_uid__":"rule"}`); r.Status != 400 {
		t.Fatalf("missing instance: %d", r.Status)
	}
	q := url.Values{"matchers": {`{"__alert_rule_uid__":"rule","instance":"host"}`}, "startsAt": {now.Format(time.RFC3339)}}
	if r = call(app, http.MethodGet, "resolve?"+q.Encode(), 1, user, nil, nil); r.Status != 400 {
		t.Fatalf("matchers plus time: %d", r.Status)
	}
}

func TestListingTimeOffsetBounds(t *testing.T) {
	app, _, _ := setupTestApp(t)
	defer app.Dispose()
	user := &backend.User{Login: "reader", Role: "Viewer"}
	for _, instant := range []string{"9999-12-31T23:59:59-01:00", "0000-01-01T00:00:00+01:00"} {
		query := url.Values{"view": {"history"}, "from": {instant}, "to": {instant}}
		response := call(app, http.MethodGet, "problems?"+query.Encode(), 1, user, nil, nil)
		if response.Status != http.StatusBadRequest {
			t.Fatalf("out-of-range listing instant %s: %d", instant, response.Status)
		}
		query = url.Values{"fingerprint": {"fp-test-1"}, "startsAt": {instant}}
		response = call(app, http.MethodGet, "resolve?"+query.Encode(), 1, user, nil, nil)
		if response.Status != http.StatusBadRequest {
			t.Fatalf("out-of-range resolver instant %s: %d", instant, response.Status)
		}
	}
	instant := "2026-01-01T03:00:00.123456789+03:00"
	query := url.Values{"view": {"history"}, "from": {instant}, "to": {instant}}
	response := call(app, http.MethodGet, "problems?"+query.Encode(), 1, user, nil, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("valid offset: %d", response.Status)
	}
}
