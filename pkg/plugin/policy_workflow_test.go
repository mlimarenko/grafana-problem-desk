package plugin_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/plugin"
	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

var jsonHeaders = map[string][]string{"Content-Type": {"application/json"}}

func decodeJSON(t *testing.T, response *backend.CallResourceResponse, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body, target); err != nil {
		t.Fatalf("decode %s: %v", response.Body, err)
	}
}

func policyMutationBody(t *testing.T, baseRevision int64, doc *policy.Document) []byte {
	t.Helper()
	canonical, err := policy.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"baseRevision": baseRevision, "doc": json.RawMessage(canonical)})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestViewerPolicyReadBootstrapsRevisionOne(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	app := plugin.NewAppWithStore(backend.AppInstanceSettings{}, store, nil)
	defer app.Dispose()

	response := call(app, http.MethodGet, "policy", 1, &backend.User{Login: "viewer", Role: "Viewer"}, nil, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("viewer GET policy = %d: %s", response.Status, response.Body)
	}
	var policyResponse models.PolicyResponse
	decodeJSON(t, response, &policyResponse)
	if policyResponse.Revision != 1 || !policyResponse.DefaultsApplied || policyResponse.Doc == nil {
		t.Fatalf("bootstrap policy = %+v", policyResponse)
	}
}

func TestPolicyAndWorkflowRolesAndStatuses(t *testing.T) {
	app, _, problemID := setupTestApp(t)
	defer app.Dispose()
	orgID := int64(1)
	viewer := &backend.User{Login: "viewer", Role: "Viewer"}
	editor := &backend.User{Login: "editor", Role: "Editor"}
	admin := &backend.User{Login: "admin", Role: "Admin"}

	response := call(app, http.MethodGet, "policy", orgID, viewer, nil, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("viewer GET policy = %d", response.Status)
	}
	var policyResponse models.PolicyResponse
	decodeJSON(t, response, &policyResponse)
	if policyResponse.Revision != 1 || !policyResponse.DefaultsApplied || len(policyResponse.Doc.Levels) != 6 {
		t.Fatalf("policy = %+v", policyResponse)
	}

	for _, route := range []struct {
		method, path string
		body         []byte
	}{
		{http.MethodPut, "policy", policyMutationBody(t, 1, policy.DefaultDoc())},
		{http.MethodPost, "policy/preview", []byte(`{"samples":[]}`)},
		{http.MethodGet, "policy/revisions", nil},
	} {
		response = call(app, route.method, route.path, orgID, editor, route.body, jsonHeaders)
		if response.Status != http.StatusForbidden {
			t.Errorf("editor %s %s = %d, want 403", route.method, route.path, response.Status)
		}
	}

	response = call(app, http.MethodGet, "problems/"+problemID+"/workflow", orgID, viewer, nil, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("viewer workflow status = %d", response.Status)
	}
	var untracked models.UntrackedWorkflowStatus
	decodeJSON(t, response, &untracked)
	if untracked.Tracked || len(untracked.AvailableWorkflows) != 1 || untracked.AvailableWorkflows[0].ID != "basic" {
		t.Fatalf("untracked = %+v", untracked)
	}
	if response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/start", orgID, viewer, []byte(`{"workflow":"basic","baseRevision":1}`), jsonHeaders); response.Status != http.StatusForbidden {
		t.Fatalf("viewer start = %d", response.Status)
	}
	if response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/transition", orgID, viewer, []byte(`{"from":"new","to":"in_progress","baseRevision":1}`), jsonHeaders); response.Status != http.StatusForbidden {
		t.Fatalf("viewer transition = %d", response.Status)
	}

	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/start", orgID, editor, []byte(`{"workflow":"basic","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusOK {
		t.Fatalf("editor start = %d: %s", response.Status, response.Body)
	}
	var mutation models.WorkflowMutationResponse
	decodeJSON(t, response, &mutation)
	if mutation.Workflow == nil || mutation.Workflow.StateID != "new" || mutation.Problem == nil || mutation.Problem.WorkflowID == nil {
		t.Fatalf("start response = %+v", mutation)
	}

	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/start", orgID, editor, []byte(`{"workflow":"basic","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusConflict || !strings.Contains(string(response.Body), "already_tracked") {
		t.Fatalf("duplicate start = %d %s", response.Status, response.Body)
	}

	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/transition", orgID, editor, []byte(`{"from":"new","to":"ghost","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid transition = %d %s", response.Status, response.Body)
	}
	var invalidBody struct {
		Missing []string `json:"missing"`
		Allowed []string `json:"allowed"`
	}
	decodeJSON(t, response, &invalidBody)
	if len(invalidBody.Allowed) != 1 || invalidBody.Allowed[0] != "in_progress" {
		t.Fatalf("allowed = %+v", invalidBody)
	}

	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/transition", orgID, editor, []byte(`{"from":"new","to":"in_progress","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusOK {
		t.Fatalf("transition = %d: %s", response.Status, response.Body)
	}

	updated := policy.DefaultDoc()
	updated.Levels[0].Name.EN = "Catastrophe"
	response = call(app, http.MethodPut, "policy", orgID, admin, policyMutationBody(t, 1, updated), jsonHeaders)
	if response.Status != http.StatusOK {
		t.Fatalf("admin PUT policy = %d: %s", response.Status, response.Body)
	}
	var saved struct {
		Revision int64 `json:"revision"`
	}
	decodeJSON(t, response, &saved)
	if saved.Revision != 2 {
		t.Fatalf("saved revision = %d", saved.Revision)
	}

	response = call(app, http.MethodPut, "policy", orgID, admin, policyMutationBody(t, 1, updated), jsonHeaders)
	if response.Status != http.StatusConflict {
		t.Fatalf("stale PUT = %d: %s", response.Status, response.Body)
	}
	var stale struct {
		Error   string `json:"error"`
		Current int64  `json:"current"`
	}
	decodeJSON(t, response, &stale)
	if stale.Error != "stale_revision" || stale.Current != 2 {
		t.Fatalf("stale body = %+v", stale)
	}

	response = call(app, http.MethodGet, "policy/revisions?limit=10", orgID, admin, nil, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("revisions = %d", response.Status)
	}
	var revisions models.PolicyRevisionsResponse
	decodeJSON(t, response, &revisions)
	if len(revisions.Revisions) != 2 || revisions.Revisions[0].Revision != 2 || revisions.Revisions[0].Action != "update" {
		t.Fatalf("revisions = %+v", revisions.Revisions)
	}
	response = call(app, http.MethodGet, "policy/revisions/1", orgID, admin, nil, nil)
	if response.Status != http.StatusOK {
		t.Fatalf("revision detail = %d", response.Status)
	}
	response = call(app, http.MethodGet, "policy/revisions/99", orgID, admin, nil, nil)
	if response.Status != http.StatusNotFound {
		t.Fatalf("missing revision = %d", response.Status)
	}
}

func TestWorkflowStatusErrors(t *testing.T) {
	app, _, problemID := setupTestApp(t)
	defer app.Dispose()
	orgID := int64(1)
	editor := &backend.User{Login: "editor", Role: "Editor"}

	response := call(app, http.MethodPost, "problems/missing/workflow/start", orgID, editor, []byte(`{"workflow":"basic","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusNotFound {
		t.Fatalf("missing problem start = %d", response.Status)
	}
	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/transition", orgID, editor, []byte(`{"from":"new","to":"in_progress","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusConflict || !strings.Contains(string(response.Body), "untracked") {
		t.Fatalf("untracked transition = %d %s", response.Status, response.Body)
	}

	// The default policy has no routing match and no default workflow.
	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/start", orgID, editor, []byte(`{"baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusUnprocessableEntity || !strings.Contains(string(response.Body), "workflow") {
		t.Fatalf("unresolved workflow start = %d %s", response.Status, response.Body)
	}
}

func TestPolicyRouteBodyLimits(t *testing.T) {
	app, _, _ := setupTestApp(t)
	defer app.Dispose()
	orgID := int64(1)
	admin := &backend.User{Login: "admin", Role: "Admin"}

	padded := fmt.Sprintf(`{"baseRevision":1,"doc":{"padding":%q}}`, strings.Repeat("x", 20*1024))
	response := call(app, http.MethodPut, "policy", orgID, admin, []byte(padded), jsonHeaders)
	if response.Status == http.StatusRequestEntityTooLarge {
		t.Fatalf("20KiB policy body was rejected as too large")
	}
	if response.Status != http.StatusBadRequest {
		t.Fatalf("20KiB policy body = %d: %s", response.Status, response.Body)
	}

	huge := fmt.Sprintf(`{"baseRevision":1,"doc":{"padding":%q}}`, strings.Repeat("x", 65*1024))
	response = call(app, http.MethodPut, "policy", orgID, admin, []byte(huge), jsonHeaders)
	if response.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("65KiB policy body = %d", response.Status)
	}

	response = call(app, http.MethodPost, "policy/preview", orgID, admin, []byte(`{"samples":[`), jsonHeaders)
	if response.Status != http.StatusBadRequest {
		t.Fatalf("malformed preview = %d", response.Status)
	}

	response = call(app, http.MethodPut, "policy", orgID, admin, []byte(`{"baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusBadRequest || !strings.Contains(string(response.Body), "issues") {
		t.Fatalf("empty policy document = %d %s", response.Status, response.Body)
	}
}

func TestListingLevelMetadataAndWorkflowFilters(t *testing.T) {
	app, _, problemID := setupTestApp(t)
	defer app.Dispose()
	orgID := int64(1)
	viewer := &backend.User{Login: "viewer", Role: "Viewer"}
	editor := &backend.User{Login: "editor", Role: "Editor"}

	list := func(query string) models.ProblemsResponse {
		t.Helper()
		response := call(app, http.MethodGet, "problems?"+query, orgID, viewer, nil, nil)
		if response.Status != http.StatusOK {
			t.Fatalf("listing %s = %d: %s", query, response.Status, response.Body)
		}
		var listing models.ProblemsResponse
		decodeJSON(t, response, &listing)
		return listing
	}

	all := list("")
	if len(all.Levels) != 6 || all.SeverityKey != "severity" || all.PolicyRevision != 1 {
		t.Fatalf("listing metadata = levels:%d key:%q revision:%d", len(all.Levels), all.SeverityKey, all.PolicyRevision)
	}
	if all.Total != 1 || all.Problems[0].LevelID != "warning" || all.Summary.ByLevel["warning"] != 1 || all.Summary.ByLevel["critical"] != 0 {
		t.Fatalf("listing = %+v", all)
	}
	if list("severity=warning").Total != 1 || list("severity=critical").Total != 0 || list("severity=warn%25").Total != 0 {
		t.Fatal("severity level filter is not exact")
	}
	if list("workState=new").Total != 0 || list("workflow=basic").Total != 0 {
		t.Fatal("workflow filters matched untracked rows")
	}

	response := call(app, http.MethodPost, "problems/"+problemID+"/workflow/start", orgID, editor, []byte(`{"workflow":"basic","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusOK {
		t.Fatalf("start = %d: %s", response.Status, response.Body)
	}
	if got := list("workflow=basic"); got.Total != 1 || got.Problems[0].WorkflowID == nil || *got.Problems[0].WorkflowID != "basic" {
		t.Fatalf("workflow filter = %+v", got)
	}
	if got := list("workState=new"); got.Total != 1 {
		t.Fatalf("workState filter = %+v", got)
	}
	if got := list("workState=resolved"); got.Total != 0 {
		t.Fatalf("workState mismatch = %+v", got)
	}
	if got := list("workflow=other"); got.Total != 0 {
		t.Fatalf("workflow mismatch = %+v", got)
	}
	if got := list("state=active&workState=new"); got.Total != 1 {
		t.Fatalf("combined filters = %+v", got)
	}

	// The source state filter stays independent of the operator workflow state.
	preview := call(app, http.MethodPost, "policy/preview", orgID, &backend.User{Login: "admin", Role: "Admin"}, []byte(`{"samples":[{"labels":{"severity":"critical"},"acknowledged":true,"assignee":"owner","status":"recovered","comment":"note"}]}`), jsonHeaders)
	if preview.Status != http.StatusOK {
		t.Fatalf("preview = %d: %s", preview.Status, preview.Body)
	}
	var previewResponse models.PolicyPreviewResponse
	decodeJSON(t, preview, &previewResponse)
	if !previewResponse.Valid || len(previewResponse.Samples) != 1 || previewResponse.Samples[0].LevelID != "critical" || !previewResponse.Samples[0].LevelMatched {
		t.Fatalf("preview = %+v", previewResponse)
	}
	if len(previewResponse.Samples[0].MissingStartRequires) != 1 || previewResponse.Samples[0].MissingStartRequires[0] != "workflow" {
		t.Fatalf("preview prerequisites = %+v", previewResponse.Samples[0])
	}
}

func TestWorkflowActionsRejectStaleRenderedRevisionAndState(t *testing.T) {
	app, _, problemID := setupTestApp(t)
	defer app.Dispose()
	orgID := int64(1)
	editor := &backend.User{Login: "editor", Role: "Editor"}

	response := call(app, http.MethodPost, "problems/"+problemID+"/workflow/start", orgID, editor, []byte(`{"workflow":"basic","baseRevision":999}`), jsonHeaders)
	if response.Status != http.StatusConflict || !strings.Contains(string(response.Body), "stale_revision") {
		t.Fatalf("stale start revision = %d %s", response.Status, response.Body)
	}
	response = call(app, http.MethodGet, "problems/"+problemID+"/workflow", orgID, editor, nil, nil)
	var untracked models.UntrackedWorkflowStatus
	decodeJSON(t, response, &untracked)
	if untracked.Tracked || untracked.PolicyRevision != 1 {
		t.Fatalf("stale start wrote a workflow: %+v", untracked)
	}

	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/start", orgID, editor, []byte(`{"workflow":"basic","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusOK {
		t.Fatalf("start = %d %s", response.Status, response.Body)
	}
	response = call(app, http.MethodPost, "problems/"+problemID+"/workflow/transition", orgID, editor, []byte(`{"from":"ghost","to":"in_progress","baseRevision":1}`), jsonHeaders)
	if response.Status != http.StatusConflict || !strings.Contains(string(response.Body), "stale_state") {
		t.Fatalf("stale transition state = %d %s", response.Status, response.Body)
	}
	response = call(app, http.MethodGet, "problems/"+problemID+"/workflow", orgID, editor, nil, nil)
	var tracked models.TrackedWorkflowStatus
	decodeJSON(t, response, &tracked)
	if !tracked.Tracked || tracked.StateID != "new" {
		t.Fatalf("stale transition changed state: %+v", tracked)
	}
}
