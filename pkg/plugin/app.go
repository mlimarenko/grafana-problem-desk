package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/sanitizer"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
	problemsync "github.com/mlimarenko/grafana-problem-desk/pkg/sync"
)

const (
	maxRequestBodyBytes       = 16 * 1024 // 16 KiB limit for regular routes
	maxPolicyRequestBodyBytes = 64 * 1024 // 64 KiB envelope for policy routes
	defaultPollInterval       = 30        // seconds
	minPollInterval           = 10        // seconds
	maxPollInterval           = 3600      // seconds
)

// Ensure App implements required SDK interfaces
var (
	_ backend.CallResourceHandler   = (*App)(nil)
	_ backend.CheckHealthHandler    = (*App)(nil)
	_ instancemgmt.InstanceDisposer = (*App)(nil)
)

// App is the Grafana backend app plugin instance for Problem Desk.
type App struct {
	store           *storage.Store
	syncer          *problemsync.Syncer
	settings        backend.AppInstanceSettings
	trustedOrgID    int64
	orgOnce         sync.Once
	pollerCancel    context.CancelFunc
	pollerWg        sync.WaitGroup
	sourceValidated map[int64]bool
	mu              sync.RWMutex
}

// NewAppInstance satisfies app.InstanceFactoryFunc for Grafana SDK plugin instantiation.
func NewAppInstance(ctx context.Context, settings backend.AppInstanceSettings) (instancemgmt.Instance, error) {
	store, err := storage.Open("")
	if err != nil {
		return nil, fmt.Errorf("failed to open storage: %w", err)
	}
	return NewAppWithStore(settings, store, nil), nil
}

// NewAppWithStore creates an App instance with provided storage and syncer.
func NewAppWithStore(settings backend.AppInstanceSettings, store *storage.Store, customSyncer *problemsync.Syncer) *App {
	if customSyncer == nil {
		customSyncer = problemsync.NewSyncer(nil)
	}
	return &App{
		store:           store,
		syncer:          customSyncer,
		settings:        settings,
		sourceValidated: make(map[int64]bool),
	}
}

// Dispose cancels background polling and releases database resources.
func (a *App) Dispose() {
	a.mu.Lock()
	cancel := a.pollerCancel
	a.pollerCancel = nil
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	a.pollerWg.Wait()

	if a.store != nil {
		_ = a.store.Close()
	}
}

// CheckHealth verifies plugin readiness and database connectivity without hardcoded orgs.
func (a *App) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if a.store == nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "storage is not initialized",
		}, nil
	}
	if err := a.store.Ping(ctx); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("database ping failed: %v", err),
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Problem Desk backend is operational",
	}, nil
}

// CallResource dispatches incoming Grafana HTTP requests to their respective handlers.
func (a *App) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	pc := req.PluginContext

	// 1. Validate trusted context
	if pc.OrgID <= 0 {
		return sendJSON(sender, http.StatusUnauthorized, map[string]string{"error": "trusted organization context required"})
	}
	if pc.User == nil || strings.TrimSpace(pc.User.Login) == "" {
		return sendJSON(sender, http.StatusUnauthorized, map[string]string{"error": "user identity required"})
	}

	role := strings.ToLower(strings.TrimSpace(pc.User.Role))
	if role != "viewer" && role != "editor" && role != "admin" {
		return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "unknown or unauthorized role"})
	}

	// 2. Bound instance to one org on first trusted resource request and activate poller
	// Note: First UI page visit activates instance. We document no historic backfill/restart
	// downtime recovery guesses: on restart or activation, only the currently active snapshot is synced.
	a.orgOnce.Do(func() {
		a.trustedOrgID = pc.OrgID
		a.startPoller(pc.OrgID)
	})

	if a.trustedOrgID != pc.OrgID {
		return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "plugin instance already bound to another organization"})
	}

	actor := strings.TrimSpace(pc.User.Login)

	// 3. Parse path and query
	cleanPath := strings.Trim(req.Path, "/")
	var rawQuery string
	if idx := strings.Index(cleanPath, "?"); idx != -1 {
		rawQuery = cleanPath[idx+1:]
		cleanPath = cleanPath[:idx]
	}
	if u, err := url.Parse(req.URL); err == nil {
		if u.RawQuery != "" {
			rawQuery = u.RawQuery
		}
		if u.Path != "" {
			cleanPath = strings.Trim(u.Path, "/")
		}
	}
	cleanPath = strings.Trim(cleanPath, "/")
	queryVals, queryErr := url.ParseQuery(rawQuery)
	parts := strings.Split(cleanPath, "/")

	// 4. Per-route body bound: policy routes carry a larger envelope.
	bodyLimit := maxRequestBodyBytes
	if cleanPath == "policy" || strings.HasPrefix(cleanPath, "policy/") {
		bodyLimit = maxPolicyRequestBodyBytes
	}
	if len(req.Body) > bodyLimit {
		return sendJSON(sender, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("request body exceeds maximum limit of %dKiB", bodyLimit/1024)})
	}

	// 5. Organization policy routes
	if cleanPath == "policy" {
		switch req.Method {
		case http.MethodGet:
			return a.handleGetPolicy(ctx, pc.OrgID, sender)
		case http.MethodPut:
			if role != "admin" {
				return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "admin role required to edit the policy"})
			}
			return a.handlePutPolicy(ctx, pc.OrgID, actor, req, sender)
		default:
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	}
	if cleanPath == "policy/preview" {
		if req.Method != http.MethodPost {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		if role != "admin" {
			return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "admin role required to preview the policy"})
		}
		return a.handlePolicyPreview(ctx, pc.OrgID, req, sender)
	}
	if cleanPath == "policy/revisions" {
		if req.Method != http.MethodGet {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		if role != "admin" {
			return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "admin role required to read policy revisions"})
		}
		return a.handleListPolicyRevisions(ctx, pc.OrgID, queryVals, queryErr, sender)
	}
	if len(parts) == 3 && parts[0] == "policy" && parts[1] == "revisions" {
		if req.Method != http.MethodGet {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		if role != "admin" {
			return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "admin role required to read policy revisions"})
		}
		return a.handleGetPolicyRevision(ctx, pc.OrgID, parts[2], sender)
	}

	// Route: GET /problems
	if cleanPath == "problems" {
		if req.Method != http.MethodGet {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		if queryErr != nil || len(rawQuery) > 16*1024 {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid query"})
		}
		return a.handleGetProblems(ctx, pc.OrgID, queryVals, sender)
	}
	if cleanPath == "resolve" {
		if req.Method != http.MethodGet {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		if queryErr != nil || len(rawQuery) > 16*1024 {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid query"})
		}
		return a.handleResolve(ctx, pc.OrgID, queryVals, sender)
	}

	// Route: POST /sync
	if cleanPath == "sync" {
		if req.Method != http.MethodPost {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		if role != "admin" {
			return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "admin role required for sync"})
		}
		return a.handleSync(ctx, pc.OrgID, sender)
	}

	// Route: GET /problems/{id}/workflow and POST workflow actions
	if len(parts) >= 3 && parts[0] == "problems" && parts[2] == "workflow" {
		if len(parts) == 3 {
			if req.Method != http.MethodGet {
				return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			}
			return a.handleGetWorkflowStatus(ctx, pc.OrgID, parts[1], role, sender)
		}
		if len(parts) == 4 {
			if req.Method != http.MethodPost {
				return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			}
			if role == "viewer" {
				return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "viewer role cannot modify problems"})
			}
			switch parts[3] {
			case "start":
				return a.handleStartWorkflow(ctx, pc.OrgID, parts[1], actor, role, req, sender)
			case "transition":
				return a.handleTransitionWorkflow(ctx, pc.OrgID, parts[1], actor, role, req, sender)
			default:
				return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
			}
		}
	}

	// Route: GET /problems/{id}
	if len(parts) == 2 && parts[0] == "problems" {
		if req.Method != http.MethodGet {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
		problemID := parts[1]
		return a.handleGetProblemDetails(ctx, pc.OrgID, problemID, sender)
	}

	// Route: POST /problems/{id}/{action}
	if len(parts) == 3 && parts[0] == "problems" {
		problemID := parts[1]
		action := parts[2]

		if req.Method != http.MethodPost {
			return sendJSON(sender, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}

		// Mutations require Editor or Admin
		if role == "viewer" {
			return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "viewer role cannot modify problems"})
		}

		switch action {
		case "comments":
			return a.handleAddComment(ctx, pc.OrgID, problemID, actor, req, sender)
		case "ack":
			return a.handleAck(ctx, pc.OrgID, problemID, actor, req, sender)
		case "unack":
			return a.handleUnack(ctx, pc.OrgID, problemID, actor, req, sender)
		case "assign":
			return a.handleAssign(ctx, pc.OrgID, problemID, actor, req, sender)
		default:
			return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
		}
	}

	return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "endpoint not found"})
}

func (a *App) handleGetProblems(ctx context.Context, orgID int64, query url.Values, sender backend.CallResourceResponseSender) error {
	options, err := parseProblemQuery(query, time.Now().UTC())
	if err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid query"})
	}
	resp, err := a.store.GetProblems(ctx, orgID, options)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to list problems"})
	}

	syncStatus, err := a.store.GetSyncStatus(ctx, orgID)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to get source status"})
	}

	resp.Source = syncStatus

	return sendJSON(sender, http.StatusOK, resp)
}

func (a *App) handleGetProblemDetails(ctx context.Context, orgID int64, problemID string, sender backend.CallResourceResponseSender) error {
	problem, err := a.store.GetProblemByID(ctx, orgID, problemID)
	if errors.Is(err, storage.ErrNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to get problem"})
	}

	comments, err := a.store.GetComments(ctx, orgID, problemID)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to get comments"})
	}

	events, err := a.store.GetEvents(ctx, orgID, problemID)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to get events"})
	}

	resp := models.ProblemDetailsResponse{
		Problem:  *problem,
		Comments: comments,
		Events:   events,
	}

	return sendJSON(sender, http.StatusOK, resp)
}

func (a *App) handleAddComment(ctx context.Context, orgID int64, problemID, actor string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !checkContentTypeJSON(req) {
		return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
	}

	var payload models.CreateCommentRequest
	if err := decodeJSONStrict(req.Body, &payload); err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid json payload: %v", err)})
	}

	body := sanitizer.NormalizePlainText(payload.Body)
	if len(body) == 0 {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "comment body cannot be empty"})
	}
	if len(body) > 5000 {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "comment body exceeds maximum 5000 characters"})
	}

	comment, err := a.store.AddComment(ctx, orgID, problemID, body, actor)
	if errors.Is(err, storage.ErrNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to add comment"})
	}

	return sendJSON(sender, http.StatusCreated, comment)
}

func (a *App) handleAck(ctx context.Context, orgID int64, problemID, actor string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	var commentText string
	if len(req.Body) > 0 {
		if !checkContentTypeJSON(req) {
			return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		}
		var payload models.AckRequest
		if err := decodeJSONStrict(req.Body, &payload); err != nil {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid json payload: %v", err)})
		}
		commentText = sanitizer.NormalizePlainText(payload.Comment)
		if len(commentText) > 5000 {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "comment exceeds maximum 5000 characters"})
		}
	}

	problem, err := a.store.AcknowledgeProblem(ctx, orgID, problemID, actor, commentText)
	if errors.Is(err, storage.ErrNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to acknowledge problem"})
	}

	return sendJSON(sender, http.StatusOK, problem)
}

func (a *App) handleUnack(ctx context.Context, orgID int64, problemID, actor string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if len(req.Body) > 0 {
		if !checkContentTypeJSON(req) {
			return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
		}
		// Empty JSON object allowed
		var empty struct{}
		if err := decodeJSONStrict(req.Body, &empty); err != nil {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid json payload: %v", err)})
		}
	}

	problem, err := a.store.UnacknowledgeProblem(ctx, orgID, problemID, actor)
	if errors.Is(err, storage.ErrNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to unacknowledge problem"})
	}

	return sendJSON(sender, http.StatusOK, problem)
}

func (a *App) handleAssign(ctx context.Context, orgID int64, problemID, actor string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !checkContentTypeJSON(req) {
		return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
	}

	var payload models.AssignRequest
	if err := decodeJSONStrict(req.Body, &payload); err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid json payload: %v", err)})
	}

	assignee := sanitizer.NormalizePlainText(payload.Assignee)
	if len(assignee) > 255 {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "assignee exceeds maximum 255 characters"})
	}

	problem, err := a.store.AssignProblem(ctx, orgID, problemID, actor, assignee)
	if errors.Is(err, storage.ErrNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to assign problem"})
	}

	return sendJSON(sender, http.StatusOK, problem)
}

func (a *App) handleSync(ctx context.Context, orgID int64, sender backend.CallResourceResponseSender) error {
	cfg, sec := a.getAppConfig()
	if cfg.GrafanaURL == "" || sec.GrafanaToken == "" {
		errResp := models.SyncResponse{
			Success: false,
			Error:   "source app configuration incomplete (grafanaUrl and grafanaToken required)",
			Source: models.SourceStatus{
				LastSuccess: "",
				Error:       "grafanaUrl and grafanaToken required",
			},
		}
		return sendJSON(sender, http.StatusBadRequest, errResp)
	}

	// Use independent timeout context so transient frontend request cancellations
	// do not abort or corrupt source synchronization.
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer syncCancel()

	err := a.syncer.Sync(syncCtx, a.store, orgID, cfg.GrafanaURL, sec.GrafanaToken)
	syncStatus, _ := a.store.GetSyncStatus(syncCtx, orgID)

	if err != nil {
		resp := models.SyncResponse{
			Success: false,
			Error:   err.Error(),
			Source:  syncStatus,
		}
		// Explicit source errors non-2xx
		return sendJSON(sender, http.StatusBadGateway, resp)
	}

	resp := models.SyncResponse{
		Success: true,
		Source:  syncStatus,
	}
	a.markSourceValidated(orgID)
	return sendJSON(sender, http.StatusOK, resp)
}

func (a *App) getAppConfig() (models.AppConfig, models.SecureConfig) {
	var cfg models.AppConfig
	if len(a.settings.JSONData) > 0 {
		_ = json.Unmarshal(a.settings.JSONData, &cfg)
	}
	var sec models.SecureConfig
	if a.settings.DecryptedSecureJSONData != nil {
		sec.GrafanaToken = a.settings.DecryptedSecureJSONData["grafanaToken"]
	}
	return cfg, sec
}

func (a *App) startPoller(orgID int64) {
	cfg, _ := a.getAppConfig()
	interval := cfg.PollIntervalSeconds
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if interval < minPollInterval {
		interval = minPollInterval
	}
	if interval > maxPollInterval {
		interval = maxPollInterval
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.pollerCancel = cancel
	a.mu.Unlock()

	a.pollerWg.Add(1)
	go func() {
		defer a.pollerWg.Done()

		// Initial sync on activation
		_ = a.executeSync(ctx, orgID)

		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = a.executeSync(ctx, orgID)
			}
		}
	}()
}

func (a *App) executeSync(ctx context.Context, orgID int64) error {
	cfg, sec := a.getAppConfig()
	if cfg.GrafanaURL == "" || sec.GrafanaToken == "" {
		return nil
	}
	if err := a.syncer.Sync(ctx, a.store, orgID, cfg.GrafanaURL, sec.GrafanaToken); err != nil {
		return err
	}
	a.markSourceValidated(orgID)
	return nil
}

// markSourceValidated records that a successful source sync happened in this process.
func (a *App) markSourceValidated(orgID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sourceValidated == nil {
		a.sourceValidated = make(map[int64]bool)
	}
	a.sourceValidated[orgID] = true
}

// sourceIsValidated reports whether this process has completed a successful sync.
func (a *App) sourceIsValidated(orgID int64) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sourceValidated[orgID]
}

func checkContentTypeJSON(req *backend.CallResourceRequest) bool {
	ct := req.GetHTTPHeader("Content-Type")
	if ct == "" {
		// If headers map was passed without canonicalization
		if val, ok := req.Headers["Content-Type"]; ok && len(val) > 0 {
			ct = val[0]
		}
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/json")
}

func decodeJSONStrict(data []byte, target any) error {
	if len(data) == 0 {
		return errors.New("empty request body")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected trailing JSON data")
	}
	return nil
}

func sendJSON(sender backend.CallResourceResponseSender, status int, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Headers: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: []byte(`{"error":"internal server error"}`),
		})
	}
	return sender.Send(&backend.CallResourceResponse{
		Status: status,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: body,
	})
}
