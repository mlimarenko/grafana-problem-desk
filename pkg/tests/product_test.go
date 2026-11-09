package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/plugin"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
	problemsync "github.com/mlimarenko/grafana-problem-desk/pkg/sync"
)

type testSender struct {
	resp *backend.CallResourceResponse
}

func (s *testSender) Send(resp *backend.CallResourceResponse) error {
	s.resp = resp
	return nil
}

func call(app *plugin.App, method, path string, orgID int64, user *backend.User, body []byte, headers map[string][]string) *backend.CallResourceResponse {
	sender := &testSender{}
	req := &backend.CallResourceRequest{
		PluginContext: backend.PluginContext{
			OrgID: orgID,
			User:  user,
		},
		Method:  method,
		Path:    path,
		URL:     path,
		Headers: headers,
		Body:    body,
	}
	_ = app.CallResource(context.Background(), req, sender)
	return sender.resp
}

func TestProductLifecycle_FullFlow(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "problemdesk.db")

	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	var alertsResponse atomic.Value
	t1 := time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC)

	alertsResponse.Store(fmt.Sprintf(`[
		{
			"fingerprint": "fp-disk",
			"startsAt": "%s",
			"status": {"state": "active"},
			"labels": {"alertname": "DiskPressure", "severity": "critical"},
			"annotations": {"summary": "Root partition at 98%%"},
			"generatorURL": "http://prometheus:9090/graph"
		},
		{
			"fingerprint": "fp-cpu",
			"startsAt": "%s",
			"status": {"state": "active"},
			"labels": {"alertname": "HighCPU", "severity": "warning"},
			"annotations": {"summary": "CPU utilization 88%%"},
			"generatorURL": "http://prometheus:9090/graph"
		}
	]`, t1.Format(time.RFC3339), t1.Format(time.RFC3339)))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/org":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 1, "name": "Primary Org"}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(alertsResponse.Load().(string)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	settings := backend.AppInstanceSettings{
		JSONData: []byte(fmt.Sprintf(`{"grafanaUrl": %q, "pollIntervalSeconds": 15}`, server.URL)),
		DecryptedSecureJSONData: map[string]string{
			"grafanaToken": "valid-grafana-token",
		},
	}

	syncer := problemsync.NewSyncer(nil)
	app := plugin.NewAppWithStore(settings, store, syncer)
	defer app.Dispose()

	orgID := int64(1)
	admin := &backend.User{Login: "alex-admin", Role: "Admin"}
	editor := &backend.User{Login: "maria-editor", Role: "Editor"}
	jsonHeaders := map[string][]string{"Content-Type": {"application/json"}}

	// 1. Admin triggers initial sync via POST /sync
	respSync := call(app, http.MethodPost, "sync", orgID, admin, nil, nil)
	if respSync.Status != http.StatusOK {
		t.Fatalf("expected 200 OK for sync, got %d: %s", respSync.Status, string(respSync.Body))
	}

	var syncResp models.SyncResponse
	if err := json.Unmarshal(respSync.Body, &syncResp); err != nil {
		t.Fatalf("failed to decode sync response: %v", err)
	}
	if !syncResp.Success || syncResp.Source.LastSuccess == "" || syncResp.Source.Error != "" {
		t.Fatalf("expected successful sync response, got %+v", syncResp)
	}

	// 2. Query problems with pagination and filtering
	respList := call(app, http.MethodGet, "problems?state=active&severity=critical&limit=10&offset=0", orgID, editor, nil, nil)
	if respList.Status != http.StatusOK {
		t.Fatalf("expected 200 OK for list, got %d", respList.Status)
	}

	var listResp models.ProblemsResponse
	if err := json.Unmarshal(respList.Body, &listResp); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Problems) != 1 {
		t.Fatalf("expected 1 critical problem, got %d", listResp.Total)
	}
	diskProblem := listResp.Problems[0]
	if diskProblem.Fingerprint != "fp-disk" {
		t.Fatalf("expected fp-disk, got %s", diskProblem.Fingerprint)
	}
	if listResp.Source.LastSuccess == "" {
		t.Fatalf("expected source.lastSuccess to be populated")
	}

	// 3. Search filter case-insensitive
	respSearch := call(app, http.MethodGet, "problems?search=ROOT", orgID, editor, nil, nil)
	var searchResp models.ProblemsResponse
	_ = json.Unmarshal(respSearch.Body, &searchResp)
	if searchResp.Total != 1 || searchResp.Problems[0].ID != diskProblem.ID {
		t.Fatalf("search for 'ROOT' failed to match summary, got total %d", searchResp.Total)
	}

	// 4. Editor acknowledges problem
	ackPayload := []byte(`{"comment":"Investigating disk space"}`)
	respAck := call(app, http.MethodPost, "problems/"+diskProblem.ID+"/ack", orgID, editor, ackPayload, jsonHeaders)
	if respAck.Status != http.StatusOK {
		t.Fatalf("expected 200 OK on ack, got %d: %s", respAck.Status, string(respAck.Body))
	}
	var ackedProb models.Problem
	_ = json.Unmarshal(respAck.Body, &ackedProb)
	if ackedProb.AcknowledgedBy != "maria-editor" || ackedProb.AcknowledgedAt == "" {
		t.Fatalf("expected acknowledgedBy maria-editor, got %+v", ackedProb)
	}

	// 5. Editor adds a follow-up comment
	commPayload := []byte(`{"body":"Log rotation freed 5GB"}`)
	respComm := call(app, http.MethodPost, "problems/"+diskProblem.ID+"/comments", orgID, editor, commPayload, jsonHeaders)
	if respComm.Status != http.StatusCreated {
		t.Fatalf("expected 201 Created on comment, got %d", respComm.Status)
	}

	// 6. Get problem details: verifies problem, comments, and audit events
	respDetails := call(app, http.MethodGet, "problems/"+diskProblem.ID, orgID, editor, nil, nil)
	if respDetails.Status != http.StatusOK {
		t.Fatalf("expected 200 OK on details, got %d", respDetails.Status)
	}
	var details models.ProblemDetailsResponse
	if err := json.Unmarshal(respDetails.Body, &details); err != nil {
		t.Fatalf("failed to decode details: %v", err)
	}
	if details.Problem.AcknowledgedBy != "maria-editor" {
		t.Errorf("expected acknowledgedBy maria-editor in details")
	}
	if len(details.Comments) < 2 {
		t.Errorf("expected at least 2 comments (ack note + follow-up), got %d", len(details.Comments))
	}
	if len(details.Events) < 1 {
		t.Errorf("expected audit events in details, got %d", len(details.Events))
	}

	// 7. Alert recovers: snapshot has 0 alerts
	alertsResponse.Store(`[]`)
	respSyncRecover := call(app, http.MethodPost, "sync", orgID, admin, nil, nil)
	if respSyncRecover.Status != http.StatusOK {
		t.Fatalf("expected 200 OK on recovery sync")
	}

	respRecoveredDetails := call(app, http.MethodGet, "problems/"+diskProblem.ID, orgID, editor, nil, nil)
	var recDetails models.ProblemDetailsResponse
	_ = json.Unmarshal(respRecoveredDetails.Body, &recDetails)
	if recDetails.Problem.Status != "recovered" || recDetails.Problem.EndsAt == "" {
		t.Fatalf("expected problem to be marked recovered with endsAt, got %+v", recDetails.Problem)
	}

	// 8. Recurrence: Alert fires again at a later time t2
	t2 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	alertsResponse.Store(fmt.Sprintf(`[
		{
			"fingerprint": "fp-disk",
			"startsAt": "%s",
			"status": {"state": "active"},
			"labels": {"alertname": "DiskPressure", "severity": "critical"},
			"annotations": {"summary": "Root partition full again"},
			"generatorURL": "http://prometheus:9090/graph"
		}
	]`, t2.Format(time.RFC3339)))

	call(app, http.MethodPost, "sync", orgID, admin, nil, nil)

	respActiveList := call(app, http.MethodGet, "problems?state=active", orgID, editor, nil, nil)
	var activeResp models.ProblemsResponse
	_ = json.Unmarshal(respActiveList.Body, &activeResp)
	if activeResp.Total != 1 {
		t.Fatalf("expected 1 active problem after recurrence, got %d", activeResp.Total)
	}
	recurrentProb := activeResp.Problems[0]
	if recurrentProb.ID == diskProblem.ID {
		t.Fatalf("recurrent episode must have distinct ID, got same %s", recurrentProb.ID)
	}
	if recurrentProb.AcknowledgedBy != "" {
		t.Fatalf("recurrent episode must NOT inherit ACK, got %q", recurrentProb.AcknowledgedBy)
	}
}

func TestProduct_SourceFailureAndTruncatedStream_NoFalseRecovery(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "problemdesk.db")

	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	var responseStatus atomic.Int32
	var responseBody atomic.Value

	responseStatus.Store(http.StatusOK)
	responseBody.Store(`[
		{
			"fingerprint": "fp-safety-1",
			"startsAt": "2026-11-09T05:00:00Z",
			"status": {"state": "active"},
			"labels": {"alertname": "DatabaseDeadlock"},
			"annotations": {"summary": "Deadlocks detected"},
			"generatorURL": "http://prometheus"
		}
	]`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/org":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 1, "name": "Org1"}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(int(responseStatus.Load()))
			w.Write([]byte(responseBody.Load().(string)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	settings := backend.AppInstanceSettings{
		JSONData: []byte(fmt.Sprintf(`{"grafanaUrl": %q, "pollIntervalSeconds": 30}`, server.URL)),
		DecryptedSecureJSONData: map[string]string{
			"grafanaToken": "test-token",
		},
	}

	syncer := problemsync.NewSyncer(nil)
	app := plugin.NewAppWithStore(settings, store, syncer)
	defer app.Dispose()

	orgID := int64(1)
	admin := &backend.User{Login: "admin", Role: "Admin"}

	// 1. Initial successful sync
	resp1 := call(app, http.MethodPost, "sync", orgID, admin, nil, nil)
	if resp1.Status != http.StatusOK {
		t.Fatalf("initial sync failed: %d", resp1.Status)
	}

	// 2. Source now returns TRUNCATED JSON (cut off stream)
	responseStatus.Store(http.StatusOK)
	responseBody.Store(`[{"fingerprint": "fp-safety-1", "startsAt": "2026-11-09T05:00:00Z", `)

	respTrunc := call(app, http.MethodPost, "sync", orgID, admin, nil, nil)
	if respTrunc.Status != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway on truncated JSON, got %d", respTrunc.Status)
	}

	// Verify problem is STILL active (NO false recovery!)
	respCheck1 := call(app, http.MethodGet, "problems?state=active", orgID, admin, nil, nil)
	var list1 models.ProblemsResponse
	_ = json.Unmarshal(respCheck1.Body, &list1)
	if list1.Total != 1 || list1.Problems[0].Status != "active" {
		t.Fatalf("CRITICAL: truncated JSON caused false recovery! Total active: %d", list1.Total)
	}

	// 3. Source now returns 500 Internal Server Error
	responseStatus.Store(http.StatusInternalServerError)
	responseBody.Store(`Internal server error`)

	resp500 := call(app, http.MethodPost, "sync", orgID, admin, nil, nil)
	if resp500.Status != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway on 500 error, got %d", resp500.Status)
	}

	// Verify problem is STILL active (NO false recovery!)
	respCheck2 := call(app, http.MethodGet, "problems?state=active", orgID, admin, nil, nil)
	var list2 models.ProblemsResponse
	_ = json.Unmarshal(respCheck2.Body, &list2)
	if list2.Total != 1 || list2.Problems[0].Status != "active" {
		t.Fatalf("CRITICAL: 500 error caused false recovery! Total active: %d", list2.Total)
	}
}

func TestProduct_FractionalStartsAt_DistinctEpisodes(t *testing.T) {
	dbDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dbDir, "problemdesk.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/org":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 1}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			w.Header().Set("Content-Type", "application/json")
			// Two distinct occurrences of the same alert starting within the same second (200ms apart)
			w.Write([]byte(`[
				{
					"fingerprint": "fp-subsecond",
					"startsAt": "2026-11-09T10:00:00.100Z",
					"status": {"state": "active"},
					"labels": {"alertname": "FlappingAlert"}
				},
				{
					"fingerprint": "fp-subsecond",
					"startsAt": "2026-11-09T10:00:00.300Z",
					"status": {"state": "active"},
					"labels": {"alertname": "FlappingAlert"}
				}
			]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	settings := backend.AppInstanceSettings{
		JSONData:                []byte(fmt.Sprintf(`{"grafanaUrl": %q}`, server.URL)),
		DecryptedSecureJSONData: map[string]string{"grafanaToken": "token"},
	}

	syncer := problemsync.NewSyncer(nil)
	app := plugin.NewAppWithStore(settings, store, syncer)
	defer app.Dispose()

	orgID := int64(1)
	admin := &backend.User{Login: "admin", Role: "Admin"}

	respSync := call(app, http.MethodPost, "sync", orgID, admin, nil, nil)
	if respSync.Status != http.StatusOK {
		t.Fatalf("sync failed: %d", respSync.Status)
	}

	// Verify both episodes exist and did not collide
	respList := call(app, http.MethodGet, "problems?state=active", orgID, admin, nil, nil)
	var list models.ProblemsResponse
	_ = json.Unmarshal(respList.Body, &list)

	if list.Total != 2 || len(list.Problems) != 2 {
		t.Fatalf("expected 2 distinct episodes for sub-second startsAt, got %d", list.Total)
	}
	if list.Problems[0].ID == list.Problems[1].ID {
		t.Fatalf("sub-second startsAt collided into same episode ID %s", list.Problems[0].ID)
	}
}

func TestProduct_LiteralUnsafeLookingTextRoundtrip(t *testing.T) {
	dbDir := t.TempDir()
	store, err := storage.Open(filepath.Join(dbDir, "problemdesk.db"))
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	literalAnnotation := "CPU load < 2 > 1 and <script>console.log('test')</script>"
	literalLabel := "cluster<alpha>"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/org":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id": 1}`))
		case "/api/alertmanager/grafana/api/v2/alerts":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(fmt.Sprintf(`[
				{
					"fingerprint": "fp-literal",
					"startsAt": "2026-11-09T10:00:00Z",
					"status": {"state": "active"},
					"labels": {"alertname": %q},
					"annotations": {"summary": %q}
				}
			]`, literalLabel, literalAnnotation)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	settings := backend.AppInstanceSettings{
		JSONData:                []byte(fmt.Sprintf(`{"grafanaUrl": %q}`, server.URL)),
		DecryptedSecureJSONData: map[string]string{"grafanaToken": "token"},
	}

	app := plugin.NewAppWithStore(settings, store, problemsync.NewSyncer(nil))
	defer app.Dispose()

	orgID := int64(1)
	editor := &backend.User{Login: "editor", Role: "Editor"}
	admin := &backend.User{Login: "admin", Role: "Admin"}
	jsonHeaders := map[string][]string{"Content-Type": {"application/json"}}

	// Sync
	call(app, http.MethodPost, "sync", orgID, admin, nil, nil)

	// List
	respList := call(app, http.MethodGet, "problems?state=active", orgID, editor, nil, nil)
	var list models.ProblemsResponse
	_ = json.Unmarshal(respList.Body, &list)
	if list.Total != 1 {
		t.Fatalf("expected 1 problem, got %d", list.Total)
	}
	prob := list.Problems[0]

	// Label and annotation must be preserved verbatim
	if prob.Labels["alertname"] != literalLabel {
		t.Errorf("label mangled: got %q, expected %q", prob.Labels["alertname"], literalLabel)
	}
	if prob.Annotations["summary"] != literalAnnotation {
		t.Errorf("annotation mangled: got %q, expected %q", prob.Annotations["summary"], literalAnnotation)
	}

	// Add comment with literal operator and markup characters
	commentPayload := `{"body":"operator check: disk < 5GB > 1GB, check <script>alert(1)</script>"}`
	respComm := call(app, http.MethodPost, "problems/"+prob.ID+"/comments", orgID, editor, []byte(commentPayload), jsonHeaders)
	if respComm.Status != http.StatusCreated {
		t.Fatalf("expected 201 Created on comment, got %d", respComm.Status)
	}

	// Fetch details
	respDetails := call(app, http.MethodGet, "problems/"+prob.ID, orgID, editor, nil, nil)
	var details models.ProblemDetailsResponse
	_ = json.Unmarshal(respDetails.Body, &details)

	if len(details.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(details.Comments))
	}
	expectedCommentBody := "operator check: disk < 5GB > 1GB, check <script>alert(1)</script>"
	if details.Comments[0].Body != expectedCommentBody {
		t.Errorf("comment body mangled: got %q, expected %q", details.Comments[0].Body, expectedCommentBody)
	}
}
