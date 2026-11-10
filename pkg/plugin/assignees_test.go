package plugin_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
)

func TestAssigneeCatalogRoutes(t *testing.T) {
	app, _, id := setupTestApp(t)
	defer app.Dispose()
	admin := &backend.User{Login: "admin", Role: "Admin"}
	editor := &backend.User{Login: "editor", Role: "Editor"}
	viewer := &backend.User{Login: "viewer", Role: "Viewer"}
	headers := map[string][]string{"Content-Type": {"application/json"}}
	request := func(method, path string, user *backend.User, body string) *backend.CallResourceResponse {
		return call(app, method, path, 1, user, []byte(body), headers)
	}
	if r := request("GET", "assignees", viewer, ""); r.Status != 403 {
		t.Fatalf("viewer: %d", r.Status)
	}
	r := request("GET", "assignees", editor, "")
	var initial models.AssigneeCatalog
	if r.Status != 200 || json.Unmarshal(r.Body, &initial) != nil || initial.Revision != 0 || initial.Members == nil || len(initial.Members) != 0 {
		t.Fatalf("initial: %s", r.Body)
	}
	body := `{"orgId":1,"baseRevision":0,"members":[{"uid":"u","login":" A ","name":"Alice"}]}`
	if r = request("PUT", "assignees", editor, body); r.Status != 403 {
		t.Fatalf("editor publish: %d", r.Status)
	}
	if r = request("PUT", "assignees", admin, strings.Repeat(" ", 4*1024*1024+1)); r.Status != 413 {
		t.Fatalf("catalog limit: %d", r.Status)
	}
	if r = request("POST", "problems/"+id+"/assign", editor, strings.Repeat(" ", 16*1024+1)); r.Status != 413 {
		t.Fatalf("regular limit: %d", r.Status)
	}
	for _, bad := range []string{`{"orgId":2,"baseRevision":0,"members":[]}`, `{"orgId":1,"baseRevision":0,"members":[{"uid":"u","login":"a","name":"","email":"private"}]}`, `{"orgId":1,"baseRevision":0,"members":[{"uid":"u","login":"a\n","name":""}]}`, `{"orgId":1,"baseRevision":0,"members":[{"uid":"u","login":"a","name":""},{"uid":"v","login":"a","name":""}]}`} {
		if r = request("PUT", "assignees", admin, bad); r.Status != 400 && r.Status != 403 {
			t.Fatalf("invalid: %d %s", r.Status, r.Body)
		}
	}
	if r = request("PUT", "assignees", admin, body); r.Status != 200 {
		t.Fatalf("publish: %d %s", r.Status, r.Body)
	}
	if r = request("PUT", "assignees", admin, body); r.Status != 409 {
		t.Fatalf("stale: %d", r.Status)
	}
	if r = request("POST", "problems/"+id+"/assign", editor, `{"assignee":"A"}`); r.Status != 422 {
		t.Fatalf("literal mismatch: %d", r.Status)
	}
	if r = request("POST", "problems/"+id+"/assign", editor, `{"assignee":" A "}`); r.Status != 200 {
		t.Fatalf("literal assignment: %d %s", r.Status, r.Body)
	}
}

func TestAssigneeUnicodeBoundary(t *testing.T) {
	app, _, id := setupTestApp(t)
	defer app.Dispose()
	admin := &backend.User{Login: "admin", Role: "Admin"}
	editor := &backend.User{Login: "editor", Role: "Editor"}
	headers := map[string][]string{"Content-Type": {"application/json"}}
	request := func(method, path string, user *backend.User, body []byte) *backend.CallResourceResponse {
		return call(app, method, path, 1, user, body, headers)
	}
	put := func(login []byte) *backend.CallResourceResponse {
		body := append([]byte(`{"orgId":1,"baseRevision":0,"members":[{"uid":"u","login":"`), login...)
		body = append(body, []byte(`","name":""}]}`)...)
		return request("PUT", "assignees", admin, body)
	}
	assign := func(login []byte) *backend.CallResourceResponse {
		body := append([]byte(`{"assignee":"`), login...)
		body = append(body, []byte(`"}`)...)
		return request("POST", "problems/"+id+"/assign", editor, body)
	}
	for _, invalid := range [][]byte{{0xff}, []byte(`\ud800`), []byte(`\udc00`), []byte(`\ud800\u0041`)} {
		if r := put(invalid); r.Status != 400 {
			t.Fatalf("invalid catalog %q: %d %s", invalid, r.Status, r.Body)
		}
		if r := assign(invalid); r.Status != 400 {
			t.Fatalf("invalid assignment %q: %d %s", invalid, r.Status, r.Body)
		}
	}
	if r := request("GET", "assignees", editor, nil); r.Status != 200 {
		t.Fatalf("get: %d", r.Status)
	} else {
		var c models.AssigneeCatalog
		if err := json.Unmarshal(r.Body, &c); err != nil || c.Revision != 0 || len(c.Members) != 0 {
			t.Fatalf("invalid publish changed catalog: %s", r.Body)
		}
	}
	if r := request("GET", "problems/"+id, editor, nil); r.Status != 200 {
		t.Fatalf("details: %d", r.Status)
	} else {
		var d models.ProblemDetailsResponse
		if err := json.Unmarshal(r.Body, &d); err != nil || d.Problem.Assignee != "" || len(d.Events) != 0 {
			t.Fatalf("invalid assignment changed state: %s", r.Body)
		}
	}
	if r := put([]byte(`\ud83d\ude00`)); r.Status != 200 {
		t.Fatalf("astral publish: %d %s", r.Status, r.Body)
	}
	if r := assign([]byte(`\ud83d\ude00`)); r.Status != 200 {
		t.Fatalf("astral assign: %d %s", r.Status, r.Body)
	} else {
		var p models.Problem
		if err := json.Unmarshal(r.Body, &p); err != nil || p.Assignee != "😀" {
			t.Fatalf("astral value: %s", r.Body)
		}
	}
	second := []byte(`{"orgId":1,"baseRevision":1,"members":[{"uid":"u2","login":"\\ud800","name":""}]}`)
	if r := request("PUT", "assignees", admin, second); r.Status != 200 {
		t.Fatalf("literal publish: %d %s", r.Status, r.Body)
	}
	if r := assign([]byte(`\\ud800`)); r.Status != 200 {
		t.Fatalf("literal assign: %d %s", r.Status, r.Body)
	} else {
		var p models.Problem
		if err := json.Unmarshal(r.Body, &p); err != nil || p.Assignee != `\ud800` {
			t.Fatalf("literal value: %s", r.Body)
		}
	}
}
