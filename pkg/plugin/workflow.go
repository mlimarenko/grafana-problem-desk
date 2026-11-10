package plugin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/sanitizer"
	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

const maxWorkflowCommentLength = 5000

func (a *App) handleGetWorkflowStatus(ctx context.Context, orgID int64, problemID, actorRole string, sender backend.CallResourceResponseSender) error {
	status, err := a.store.WorkflowStatus(ctx, orgID, problemID, actorRole, time.Now().UTC(), a.sourceIsValidated(orgID))
	if errors.Is(err, storage.ErrNotFound) {
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
	}
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to read workflow status"})
	}
	return sendJSON(sender, http.StatusOK, status)
}

func (a *App) handleStartWorkflow(ctx context.Context, orgID int64, problemID, actor, actorRole string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !checkContentTypeJSON(req) {
		return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
	}
	var payload models.StartWorkflowRequest
	if err := decodeJSONStrict(req.Body, &payload); err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
	}
	if payload.BaseRevision <= 0 {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "baseRevision is required"})
	}
	comment := sanitizer.NormalizePlainText(payload.Comment)
	if len(comment) > maxWorkflowCommentLength {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "comment exceeds maximum 5000 characters"})
	}
	result, err := a.store.StartWorkflow(ctx, orgID, problemID, actor, actorRole, payload.Workflow, comment, payload.BaseRevision, time.Now().UTC(), a.sourceIsValidated(orgID))
	if err != nil {
		var missing *storage.MissingWorkflowError
		var stale *storage.StaleRevisionError
		var routed *storage.RoutingConflictError
		switch {
		case errors.Is(err, storage.ErrNotFound):
			return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
		case errors.Is(err, storage.ErrAlreadyTracked):
			return sendJSON(sender, http.StatusConflict, map[string]string{"error": "already_tracked"})
		case errors.Is(err, storage.ErrRoleRequired):
			return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "admin_role_required"})
		case errors.As(err, &stale):
			return sendJSON(sender, http.StatusConflict, map[string]any{"error": "stale_revision", "current": stale.Current})
		case errors.As(err, &routed):
			return sendJSON(sender, http.StatusConflict, map[string]any{"error": "workflow_selected", "workflow": routed.Routed})
		case errors.As(err, &missing):
			return sendJSON(sender, http.StatusUnprocessableEntity, map[string]any{"missing": missing.Missing})
		default:
			return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to start workflow"})
		}
	}
	return sendJSON(sender, http.StatusOK, result)
}

func (a *App) handleTransitionWorkflow(ctx context.Context, orgID int64, problemID, actor, actorRole string, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if !checkContentTypeJSON(req) {
		return sendJSON(sender, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"})
	}
	var payload models.TransitionWorkflowRequest
	if err := decodeJSONStrict(req.Body, &payload); err != nil {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "invalid json payload"})
	}
	if payload.From == "" || payload.To == "" || payload.BaseRevision <= 0 {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "from, to and baseRevision are required"})
	}
	comment := sanitizer.NormalizePlainText(payload.Comment)
	if len(comment) > maxWorkflowCommentLength {
		return sendJSON(sender, http.StatusBadRequest, map[string]string{"error": "comment exceeds maximum 5000 characters"})
	}
	result, err := a.store.TransitionWorkflow(ctx, orgID, problemID, actor, actorRole, payload.From, payload.To, comment, payload.BaseRevision, time.Now().UTC(), a.sourceIsValidated(orgID))
	if err != nil {
		var invalid *storage.InvalidTransitionError
		var stale *storage.StaleRevisionError
		switch {
		case errors.Is(err, storage.ErrNotFound):
			return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "problem not found"})
		case errors.Is(err, storage.ErrUntracked):
			return sendJSON(sender, http.StatusConflict, map[string]string{"error": "untracked"})
		case errors.Is(err, storage.ErrRoleRequired):
			return sendJSON(sender, http.StatusForbidden, map[string]string{"error": "admin_role_required"})
		case errors.Is(err, storage.ErrStaleState):
			return sendJSON(sender, http.StatusConflict, map[string]string{"error": "stale_state"})
		case errors.As(err, &stale):
			return sendJSON(sender, http.StatusConflict, map[string]any{"error": "stale_revision", "current": stale.Current})
		case errors.Is(err, storage.ErrWorkflowRetired):
			return sendJSON(sender, http.StatusConflict, map[string]string{"error": "workflow_retired"})
		case errors.Is(err, storage.ErrSourceStateUntrusted):
			return sendJSON(sender, http.StatusConflict, map[string]string{"error": "source_state_untrusted"})
		case errors.As(err, &invalid):
			return sendJSON(sender, http.StatusUnprocessableEntity, map[string]any{"missing": invalid.Missing, "allowed": invalid.Allowed})
		default:
			return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": "failed to transition workflow"})
		}
	}
	return sendJSON(sender, http.StatusOK, result)
}
