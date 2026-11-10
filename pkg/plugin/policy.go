package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

func (a *App) handleGetPolicy(ctx context.Context, orgID int64, sender backend.CallResourceResponseSender) error {
	response, err := a.store.GetPolicy(ctx, orgID)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to load policy"})
	}
	return sendJSON(sender, http.StatusOK, response)
}

func (a *App) handlePutPolicy(ctx context.Context, orgID int64, actor string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !checkContentTypeJSON(req) {
		return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
	}
	var payload models.PutPolicyRequest
	if err := decodeJSONStrict(req.Body, &payload); err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
	}
	if len(payload.Doc) == 0 {
		return sendJSON(sender, http.StatusBadRequest, map[string]any{"issues": []policy.Issue{{Path: "$.doc", Code: "required", Message: "policy document is required"}}})
	}
	doc, issues := policy.ParseDocument(payload.Doc)
	if len(issues) > 0 {
		return sendJSON(sender, http.StatusBadRequest, map[string]any{"issues": issues})
	}
	revision, err := a.store.PutPolicy(ctx, orgID, actor, payload.BaseRevision, doc)
	if err != nil {
		var stale *storage.PolicyStaleError
		if errors.As(err, &stale) {
			return sendJSON(sender, http.StatusConflict, map[string]any{"error": "stale_revision", "current": stale.Current})
		}
		var invalid *policy.ValidationError
		if errors.As(err, &invalid) {
			return sendJSON(sender, http.StatusBadRequest, map[string]any{"issues": invalid.Issues})
		}
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to save policy"})
	}
	return sendJSON(sender, http.StatusOK, map[string]any{"revision": revision})
}

func (a *App) handlePolicyPreview(ctx context.Context, orgID int64, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !checkContentTypeJSON(req) {
		return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
	}
	var payload models.PolicyPreviewRequest
	if err := decodeJSONStrict(req.Body, &payload); err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
	}
	active, err := a.store.ActivePolicy(ctx, orgID)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to load policy"})
	}

	doc := active.Doc
	compiled := active.Compiled
	issues := []policy.Issue{}
	if len(payload.Doc) > 0 {
		parsed, parsedIssues := policy.ParseDocument(payload.Doc)
		doc = parsed
		issues = parsedIssues
		compiled = nil
		if len(parsedIssues) == 0 {
			compiled, _ = policy.Compile(parsed, active.Revision)
		}
	}

	response := models.PolicyPreviewResponse{
		Valid:               len(issues) == 0 && compiled != nil,
		Issues:              issues,
		OccupancyViolations: []models.OccupancyEntry{},
		Samples:             []models.PreviewSampleResult{},
	}
	if doc != nil {
		occupancy, err := a.store.PolicyOccupancy(ctx, orgID)
		if err != nil {
			return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to read workflow occupancy"})
		}
		response.OccupancyViolations = storage.PolicyOccupancyViolations(doc, occupancy)
	}
	if compiled != nil {
		trust, err := a.store.SourceTrustFor(ctx, orgID, time.Now().UTC(), doc.TrustMinutes(), a.sourceIsValidated(orgID))
		if err != nil {
			return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to read source trust"})
		}
		for _, sample := range payload.Samples {
			response.Samples = append(response.Samples, previewSample(compiled, trust, sample))
		}
	}
	return sendJSON(sender, http.StatusOK, response)
}

func previewSample(compiled *policy.Compiled, trust models.WorkflowSource, sample models.PreviewSample) models.PreviewSampleResult {
	result := models.PreviewSampleResult{MissingStartRequires: []string{}}
	match := compiled.ResolveLevel(sample.Labels[compiled.Doc.SeverityKey])
	result.LevelID = match.Level.ID
	result.LevelMatched = match.Matched
	workflowID, ok := compiled.SelectWorkflow(sample.Labels, match.Level.ID)
	if !ok {
		result.MissingStartRequires = []string{"workflow"}
		return result
	}
	result.WorkflowID = workflowID
	workflow, ok := compiled.Workflow(workflowID)
	if !ok {
		result.WorkflowID = ""
		result.MissingStartRequires = []string{"workflow"}
		return result
	}
	missing := policy.MissingGates(workflow.StartRequires, policy.GateFacts{
		Acknowledged:    sample.Acknowledged,
		Owner:           sample.Assignee,
		Comment:         sample.Comment,
		SourceRecovered: sample.Status == "recovered" && trust.Trusted,
	})
	result.MissingStartRequires = missing
	result.CanStart = len(missing) == 0
	return result
}

func (a *App) handleListPolicyRevisions(ctx context.Context, orgID int64, query url.Values, queryErr error, sender backend.CallResourceResponseSender) error {
	limit := 50
	if queryErr != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid query"})
	}
	if raw, ok := query["limit"]; ok {
		if len(raw) != 1 {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid query"})
		}
		parsed, err := strconv.Atoi(raw[0])
		if err != nil || parsed < 1 || parsed > 200 {
			return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid query"})
		}
		limit = parsed
	}
	revisions, err := a.store.PolicyRevisions(ctx, orgID, limit)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to list policy revisions"})
	}
	return sendJSON(sender, http.StatusOK, models.PolicyRevisionsResponse{Revisions: revisions})
}

func (a *App) handleGetPolicyRevision(ctx context.Context, orgID int64, rawRevision string, sender backend.CallResourceResponseSender) error {
	revision, err := strconv.ParseInt(rawRevision, 10, 64)
	if err != nil || revision < 1 {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid revision"})
	}
	response, err := a.store.PolicyRevision(ctx, orgID, revision)
	if errors.Is(err, storage.ErrPolicyRevisionNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "policy revision not found"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to read policy revision"})
	}
	return sendJSON(sender, http.StatusOK, response)
}
