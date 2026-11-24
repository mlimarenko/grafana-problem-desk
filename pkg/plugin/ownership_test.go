package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
)

func TestOwnershipRoutes(t *testing.T) {
	app, store, id := setupTestApp(t)
	defer app.Dispose()
	alice := &backend.User{Login: "alice", Role: "Editor"}
	bob := &backend.User{Login: "bob", Role: "Admin"}
	viewer := &backend.User{Login: "viewer", Role: "Viewer"}
	request := func(user *backend.User, org int64, path string, body []byte, want int) *backend.CallResourceResponse {
		t.Helper()
		r := call(app, http.MethodPost, path, org, user, body, nil)
		if r.Status != want {
			t.Fatalf("%s: got %d, want %d: %s", path, r.Status, want, r.Body)
		}
		return r
	}
	claim, release := "problems/"+id+"/claim", "problems/"+id+"/release"
	request(viewer, 1, claim, nil, 403)
	request(alice, 1, claim, []byte(`{"assignee":"bob"}`), 400)
	request(alice, 1, "problems/"+id+"/assign", nil, 404)
	request(alice, 1, "assignees", nil, 404)
	r := request(alice, 1, claim, nil, 200)
	var p models.Problem
	if err := json.Unmarshal(r.Body, &p); err != nil || p.Assignee != "alice" {
		t.Fatalf("claim response: %+v %v", p, err)
	}
	request(bob, 1, claim, nil, 409)
	request(bob, 1, release, nil, 409)
	request(alice, 2, release, nil, 403)
	request(viewer, 1, release, nil, 403)
	request(alice, 1, release, nil, 200)
	request(alice, 1, release, nil, 409)
	events, err := store.GetEvents(context.Background(), 1, id)
	if err != nil {
		t.Fatal(err)
	}
	foundClaim, foundRelease := false, false
	for _, event := range events {
		if event.Kind == "assign" && event.Author == "alice" {
			foundClaim = foundClaim || event.Body == "Assigned to alice"
			foundRelease = foundRelease || event.Body == "Assignment cleared"
		}
	}
	if !foundClaim || !foundRelease {
		t.Fatalf("assignment history: %+v", events)
	}
}
