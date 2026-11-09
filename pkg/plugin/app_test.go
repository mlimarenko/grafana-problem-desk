package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/plugin"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
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

func setupTestApp(t *testing.T) (*plugin.App, *storage.Store, string) {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open memory store: %v", err)
	}

	orgID := int64(1)
	t0 := time.Now().UTC()
	alert := storage.SnapshotAlert{
		Fingerprint:  "fp-test-1",
		StartsAt:     t0.Format(time.RFC3339),
		Labels:       map[string]string{"alertname": "DiskHigh", "severity": "warning"},
		Annotations:  map[string]string{"summary": "Disk at 85%"},
		GeneratorURL: "http://prometheus",
	}
	_ = store.SyncSnapshot(context.Background(), orgID, t0, []storage.SnapshotAlert{alert})

	probs, _, _ := store.GetProblems(context.Background(), orgID, "active", "", "", 10, 0)
	if len(probs) == 0 {
		t.Fatalf("failed to seed problem")
	}
	probID := probs[0].ID

	app := plugin.NewAppWithStore(backend.AppInstanceSettings{}, store, nil)
	return app, store, probID
}

func TestViewerDenialAllMutations(t *testing.T) {
	app, _, probID := setupTestApp(t)
	defer app.Dispose()

	viewer := &backend.User{Login: "alice-viewer", Role: "Viewer"}
	orgID := int64(1)
	jsonHeaders := map[string][]string{"Content-Type": {"application/json"}}

	// Viewer CAN read
	respGetList := call(app, http.MethodGet, "problems", orgID, viewer, nil, nil)
	if respGetList.Status != http.StatusOK {
		t.Errorf("viewer should be able to GET /problems, got status %d", respGetList.Status)
	}

	respGetDetails := call(app, http.MethodGet, "problems/"+probID, orgID, viewer, nil, nil)
	if respGetDetails.Status != http.StatusOK {
		t.Errorf("viewer should be able to GET /problems/{id}, got status %d", respGetDetails.Status)
	}

	// Viewer MUST be denied all mutations with 403 Forbidden
	mutations := []struct {
		name   string
		path   string
		body   []byte
		header map[string][]string
	}{
		{
			name:   "add comment",
			path:   "problems/" + probID + "/comments",
			body:   []byte(`{"body":"viewer trying to comment"}`),
			header: jsonHeaders,
		},
		{
			name:   "ack",
			path:   "problems/" + probID + "/ack",
			body:   []byte(`{"comment":"viewer trying to ack"}`),
			header: jsonHeaders,
		},
		{
			name:   "unack",
			path:   "problems/" + probID + "/unack",
			body:   []byte(`{}`),
			header: jsonHeaders,
		},
		{
			name:   "assign",
			path:   "problems/" + probID + "/assign",
			body:   []byte(`{"assignee":"bob"}`),
			header: jsonHeaders,
		},
		{
			name:   "sync",
			path:   "sync",
			body:   nil,
			header: nil,
		},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			resp := call(app, http.MethodPost, m.path, orgID, viewer, m.body, m.header)
			if resp.Status != http.StatusForbidden {
				t.Fatalf("expected 403 Forbidden for viewer on %s, got %d", m.name, resp.Status)
			}
		})
	}
}

func TestNilIdentityAndContextDenial(t *testing.T) {
	app, _, probID := setupTestApp(t)
	defer app.Dispose()

	validUser := &backend.User{Login: "editor1", Role: "Editor"}

	// 1. Invalid OrgID <= 0
	resp := call(app, http.MethodGet, "problems", 0, validUser, nil, nil)
	if resp.Status != http.StatusUnauthorized {
		t.Errorf("expected 401 for OrgID=0, got %d", resp.Status)
	}
	respNegative := call(app, http.MethodGet, "problems", -5, validUser, nil, nil)
	if respNegative.Status != http.StatusUnauthorized {
		t.Errorf("expected 401 for OrgID=-5, got %d", respNegative.Status)
	}

	// 2. nil User
	respNilUser := call(app, http.MethodGet, "problems", 1, nil, nil, nil)
	if respNilUser.Status != http.StatusUnauthorized {
		t.Errorf("expected 401 for nil user, got %d", respNilUser.Status)
	}

	// 3. Empty User.Login
	emptyLogin := &backend.User{Login: "   ", Role: "Editor"}
	respEmptyLogin := call(app, http.MethodGet, "problems", 1, emptyLogin, nil, nil)
	if respEmptyLogin.Status != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty login, got %d", respEmptyLogin.Status)
	}

	// 4. Unknown / Absent Role
	unknownRole := &backend.User{Login: "guest", Role: "Superuser"}
	respUnknown := call(app, http.MethodGet, "problems", 1, unknownRole, nil, nil)
	if respUnknown.Status != http.StatusForbidden {
		t.Errorf("expected 403 for unknown role, got %d", respUnknown.Status)
	}

	emptyRole := &backend.User{Login: "guest", Role: ""}
	respEmptyRole := call(app, http.MethodGet, "problems/"+probID, 1, emptyRole, nil, nil)
	if respEmptyRole.Status != http.StatusForbidden {
		t.Errorf("expected 403 for empty role, got %d", respEmptyRole.Status)
	}
}

func TestForgedBodyAuthorDenial(t *testing.T) {
	app, store, probID := setupTestApp(t)
	defer app.Dispose()

	orgID := int64(1)
	editor := &backend.User{Login: "legit-user", Role: "Editor"}
	jsonHeaders := map[string][]string{"Content-Type": {"application/json"}}

	// 1. Forged author field in JSON payload must be rejected with 400 Bad Request
	forgedPayload := `{"body":"Attempted spoof","author":"fake-admin"}`
	respForged := call(app, http.MethodPost, "problems/"+probID+"/comments", orgID, editor, []byte(forgedPayload), jsonHeaders)
	if respForged.Status != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unknown field 'author', got status %d", respForged.Status)
	}

	// 2. Legitimate comment has author taken strictly from pc.User.Login
	validPayload := `{"body":"Legit discussion note"}`
	respLegit := call(app, http.MethodPost, "problems/"+probID+"/comments", orgID, editor, []byte(validPayload), jsonHeaders)
	if respLegit.Status != http.StatusCreated {
		t.Fatalf("expected 201 Created for comment, got status %d", respLegit.Status)
	}

	var comment models.Comment
	if err := json.Unmarshal(respLegit.Body, &comment); err != nil {
		t.Fatalf("failed to decode comment response: %v", err)
	}
	if comment.Author != "legit-user" {
		t.Errorf("expected author 'legit-user', got %q", comment.Author)
	}

	// Verify in database
	dbComments, err := store.GetComments(context.Background(), orgID, probID)
	if err != nil || len(dbComments) == 0 {
		t.Fatalf("failed to query db comments: %v", err)
	}
	if dbComments[len(dbComments)-1].Author != "legit-user" {
		t.Errorf("expected db comment author 'legit-user', got %q", dbComments[len(dbComments)-1].Author)
	}
}

func TestRequestBodySizeLimit(t *testing.T) {
	app, _, probID := setupTestApp(t)
	defer app.Dispose()

	orgID := int64(1)
	editor := &backend.User{Login: "editor1", Role: "Editor"}
	jsonHeaders := map[string][]string{"Content-Type": {"application/json"}}

	// 17 KiB payload (> 16 KiB limit)
	hugeBody := strings.Repeat("A", 17*1024)
	hugePayload := `{"body":"` + hugeBody + `"}`

	resp := call(app, http.MethodPost, "problems/"+probID+"/comments", orgID, editor, []byte(hugePayload), jsonHeaders)
	if resp.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 Request Entity Too Large, got %d", resp.Status)
	}
}

func TestUnsupportedMediaType(t *testing.T) {
	app, _, probID := setupTestApp(t)
	defer app.Dispose()

	orgID := int64(1)
	editor := &backend.User{Login: "editor1", Role: "Editor"}
	plainHeaders := map[string][]string{"Content-Type": {"text/plain"}}

	resp := call(app, http.MethodPost, "problems/"+probID+"/comments", orgID, editor, []byte(`{"body":"test"}`), plainHeaders)
	if resp.Status != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 Unsupported Media Type, got %d", resp.Status)
	}
}

func TestCorrectMethodEnforcement(t *testing.T) {
	app, _, probID := setupTestApp(t)
	defer app.Dispose()

	orgID := int64(1)
	admin := &backend.User{Login: "admin", Role: "Admin"}

	// POST to /problems -> 405
	resp1 := call(app, http.MethodPost, "problems", orgID, admin, nil, nil)
	if resp1.Status != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on POST /problems, got %d", resp1.Status)
	}

	// GET to /sync -> 405
	resp2 := call(app, http.MethodGet, "sync", orgID, admin, nil, nil)
	if resp2.Status != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on GET /sync, got %d", resp2.Status)
	}

	// PUT to /problems/{id}/comments -> 405
	resp3 := call(app, http.MethodPut, "problems/"+probID+"/comments", orgID, admin, nil, nil)
	if resp3.Status != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 on PUT /problems/{id}/comments, got %d", resp3.Status)
	}
}

func TestEditorMutationsAndAdminSync(t *testing.T) {
	app, _, probID := setupTestApp(t)
	defer app.Dispose()

	orgID := int64(1)
	editor := &backend.User{Login: "editor1", Role: "Editor"}
	admin := &backend.User{Login: "admin1", Role: "Admin"}
	jsonHeaders := map[string][]string{"Content-Type": {"application/json"}}

	// Editor can ACK
	respAck := call(app, http.MethodPost, "problems/"+probID+"/ack", orgID, editor, []byte(`{"comment":"ack by editor"}`), jsonHeaders)
	if respAck.Status != http.StatusOK {
		t.Fatalf("expected 200 OK on ACK by editor, got %d", respAck.Status)
	}
	var ackProblem models.Problem
	_ = json.Unmarshal(respAck.Body, &ackProblem)
	if ackProblem.AcknowledgedBy != "editor1" {
		t.Errorf("expected acknowledgedBy editor1, got %q", ackProblem.AcknowledgedBy)
	}

	// Editor can UNACK
	respUnack := call(app, http.MethodPost, "problems/"+probID+"/unack", orgID, editor, []byte(`{}`), jsonHeaders)
	if respUnack.Status != http.StatusOK {
		t.Fatalf("expected 200 OK on UNACK by editor, got %d", respUnack.Status)
	}
	var unackProblem models.Problem
	_ = json.Unmarshal(respUnack.Body, &unackProblem)
	if unackProblem.AcknowledgedBy != "" {
		t.Errorf("expected acknowledgedBy cleared, got %q", unackProblem.AcknowledgedBy)
	}

	// Editor can ASSIGN
	respAssign := call(app, http.MethodPost, "problems/"+probID+"/assign", orgID, editor, []byte(`{"assignee":"dev-oncall"}`), jsonHeaders)
	if respAssign.Status != http.StatusOK {
		t.Fatalf("expected 200 OK on ASSIGN by editor, got %d", respAssign.Status)
	}
	var assignProblem models.Problem
	_ = json.Unmarshal(respAssign.Body, &assignProblem)
	if assignProblem.Assignee != "dev-oncall" {
		t.Errorf("expected assignee dev-oncall, got %q", assignProblem.Assignee)
	}

	// Editor CANNOT sync (Admin only)
	respEditorSync := call(app, http.MethodPost, "sync", orgID, editor, nil, nil)
	if respEditorSync.Status != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for editor on POST /sync, got %d", respEditorSync.Status)
	}

	// Admin CAN call sync (even if unconfigured returns 400 Bad Request with json response, not 403)
	respAdminSync := call(app, http.MethodPost, "sync", orgID, admin, nil, nil)
	if respAdminSync.Status == http.StatusForbidden {
		t.Fatalf("admin should not receive 403 Forbidden on POST /sync")
	}
}

func TestCheckHealth(t *testing.T) {
	app, _, _ := setupTestApp(t)
	defer app.Dispose()

	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Errorf("expected HealthStatusOk, got %v", res.Status)
	}
}
