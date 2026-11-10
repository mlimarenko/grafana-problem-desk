package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

var (
	// ErrAlreadyTracked is returned when starting a workflow on a tracked episode.
	ErrAlreadyTracked = errors.New("workflow already tracked")
	// ErrUntracked is returned when transitioning an episode without a workflow.
	ErrUntracked = errors.New("workflow not tracked")
	// ErrStaleState is returned when the pinned state changed before the CAS write.
	ErrStaleState = errors.New("workflow state changed")
	// ErrWorkflowRetired is returned when the pinned workflow or state left the active policy.
	ErrWorkflowRetired = errors.New("workflow retired")
	// ErrSourceStateUntrusted is returned when a recovered claim is not backed by a trusted source.
	ErrSourceStateUntrusted = errors.New("source state is untrusted")
	// ErrRoleRequired is returned when the trusted actor role is below the configured minimum.
	ErrRoleRequired = errors.New("workflow action requires a higher role")
)

// StaleRevisionError reports that the rendered action used an older policy revision.
type StaleRevisionError struct {
	Current int64
}

func (e *StaleRevisionError) Error() string {
	return "policy revision changed"
}

// RoutingConflictError reports an explicit workflow that contradicts the
// routing/default selection for this episode.
type RoutingConflictError struct {
	Routed string
}

func (e *RoutingConflictError) Error() string {
	return "routing selected workflow " + e.Routed
}

// MissingWorkflowError reports an unresolved workflow choice or unmet start gates.
type MissingWorkflowError struct {
	Missing []string
}

func (e *MissingWorkflowError) Error() string {
	return "workflow action is missing prerequisites: " + strings.Join(e.Missing, ", ")
}

// InvalidTransitionError reports an unavailable or gated transition.
type InvalidTransitionError struct {
	Missing []string
	Allowed []string
}

func (e *InvalidTransitionError) Error() string {
	return "transition is not allowed; missing: " + strings.Join(e.Missing, ", ")
}

type sourceTrust struct {
	LastSuccess string
	Error       string
	Trusted     bool
	Stale       bool
}

type workflowRow struct {
	Status         string
	AcknowledgedBy string
	Assignee       string
	WorkflowID     sql.NullString
	StateID        sql.NullString
	Labels         map[string]string
	LevelID        string
}

// sourceTrust evaluates the fail-closed source gate inputs for an organization.
func (s *Store) sourceTrust(ctx context.Context, queryer dbQueryer, orgID int64, now time.Time, trustMinutes int, processValidated bool) (sourceTrust, error) {
	var lastSuccess, lastError string
	err := queryer.QueryRowContext(ctx, `
		SELECT last_success, last_error FROM source_sync_status WHERE org_id = ?`, orgID).Scan(&lastSuccess, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return sourceTrust{}, nil
	}
	if err != nil {
		return sourceTrust{}, fmt.Errorf("failed to read source trust: %w", err)
	}
	trust := sourceTrust{LastSuccess: lastSuccess, Error: lastError}
	if lastSuccess == "" || lastError != "" {
		return trust, nil
	}
	instant, err := time.Parse(time.RFC3339Nano, lastSuccess)
	if err != nil {
		instant, err = time.Parse(time.RFC3339, lastSuccess)
	}
	if err != nil {
		return trust, nil
	}
	if now.Sub(instant) > time.Duration(trustMinutes)*time.Minute {
		trust.Stale = true
		return trust, nil
	}
	trust.Trusted = processValidated
	return trust, nil
}

func readWorkflowRow(ctx context.Context, queryer dbQueryer, orgID int64, problemID string) (*workflowRow, error) {
	var row workflowRow
	var labels string
	err := queryer.QueryRowContext(ctx, `
		SELECT status, acknowledged_by, assignee, workflow_id, state_id, labels, level_id
		FROM problems
		WHERE org_id = ? AND id = ?`, orgID, problemID).Scan(
		&row.Status, &row.AcknowledgedBy, &row.Assignee, &row.WorkflowID, &row.StateID, &labels, &row.LevelID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read problem workflow: %w", err)
	}
	if err := json.Unmarshal([]byte(labels), &row.Labels); err != nil {
		return nil, fmt.Errorf("failed to read problem labels: %w", err)
	}
	if row.Labels == nil {
		row.Labels = map[string]string{}
	}
	return &row, nil
}

func gateFacts(row *workflowRow, comment string, trust sourceTrust) policy.GateFacts {
	return policy.GateFacts{
		Acknowledged:    strings.TrimSpace(row.AcknowledgedBy) != "",
		Owner:           row.Assignee,
		Comment:         comment,
		SourceRecovered: row.Status == "recovered" && trust.Trusted,
	}
}

// permittedTargets lists outgoing edges whose non-comment gates hold now.
func permittedTargets(compiled *policy.Compiled, workflowID, from string, facts policy.GateFacts) []string {
	allowed := make([]string, 0)
	for _, edge := range compiled.TransitionsFrom(workflowID, from) {
		if len(policy.MissingGates(edge.Requires, facts)) == 0 {
			allowed = append(allowed, edge.To)
		}
	}
	return allowed
}

// StartWorkflow pins a workflow and writes the start event atomically.
func (s *Store) StartWorkflow(ctx context.Context, orgID int64, problemID, actor, actorRole, workflowID, comment string, baseRevision int64, now time.Time, processValidated bool) (models.WorkflowMutationResponse, error) {
	if _, err := s.EnsurePolicy(ctx, orgID); err != nil {
		return models.WorkflowMutationResponse{}, err
	}
	err := s.withImmediate(ctx, func(conn *sql.Conn) error {
		row, err := readWorkflowRow(ctx, conn, orgID, problemID)
		if err != nil {
			return err
		}
		if row.WorkflowID.Valid {
			return ErrAlreadyTracked
		}
		snapshot, err := s.readLatestPolicy(ctx, conn, orgID)
		if err != nil {
			return err
		}
		if snapshot.Revision != baseRevision {
			return &StaleRevisionError{Current: snapshot.Revision}
		}
		// Stored level metadata may predate this policy revision, especially for
		// recovered episodes that will not be replayed by the source again.
		level := snapshot.Compiled.ResolveLevel(row.Labels[snapshot.Doc.SeverityKey]).Level.ID
		routed, hasSelection := snapshot.Compiled.SelectWorkflow(row.Labels, level)
		selected := workflowID
		if hasSelection {
			// A route or non-null default owns the choice for this episode: an
			// operator cannot substitute a weaker workflow id.
			if selected != "" && selected != routed {
				return &RoutingConflictError{Routed: routed}
			}
			selected = routed
		}
		if selected == "" {
			return &MissingWorkflowError{Missing: []string{"workflow"}}
		}
		workflow, ok := snapshot.Compiled.Workflow(selected)
		if !ok {
			return &MissingWorkflowError{Missing: []string{"workflow"}}
		}
		if !policy.RoleAtLeast(actorRole, workflow.MinimumRole) {
			return ErrRoleRequired
		}
		initial, ok := snapshot.Compiled.InitialState(selected)
		if !ok {
			return &MissingWorkflowError{Missing: []string{"workflow"}}
		}
		trust, err := s.sourceTrust(ctx, conn, orgID, now, snapshot.Doc.TrustMinutes(), processValidated)
		if err != nil {
			return err
		}
		facts := gateFacts(row, comment, trust)
		if missing := policy.MissingGates(workflow.StartRequires, facts); len(missing) > 0 {
			return &MissingWorkflowError{Missing: missing}
		}
		nowText := now.UTC().Format(time.RFC3339)
		result, err := conn.ExecContext(ctx, `
			UPDATE problems SET workflow_id = ?, state_id = ?, updated_at = ?
			WHERE org_id = ? AND id = ? AND workflow_id IS NULL`,
			selected, initial.ID, nowText, orgID, problemID)
		if err != nil {
			return fmt.Errorf("failed to pin workflow: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to confirm workflow pin: %w", err)
		}
		if affected != 1 {
			return ErrAlreadyTracked
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO events (id, org_id, problem_id, kind, author, created_at, body, policy_revision)
			VALUES (?, ?, ?, 'workflow_start', ?, ?, ?, ?)`,
			uuid.New().String(), orgID, problemID, actor, nowText,
			fmt.Sprintf("Workflow %s started in state %s", selected, initial.ID), snapshot.Revision); err != nil {
			return fmt.Errorf("failed to record workflow start: %w", err)
		}
		if comment != "" {
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO comments (id, org_id, problem_id, body, author, created_at)
				VALUES (?, ?, ?, ?, ?, ?)`,
				uuid.New().String(), orgID, problemID, comment, actor, nowText); err != nil {
				return fmt.Errorf("failed to store workflow start comment: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return models.WorkflowMutationResponse{}, err
	}
	return s.workflowMutationResult(ctx, orgID, problemID, actorRole, now, processValidated)
}

// TransitionWorkflow advances a pinned workflow along an explicit active-document edge.
func (s *Store) TransitionWorkflow(ctx context.Context, orgID int64, problemID, actor, actorRole, from, to, comment string, baseRevision int64, now time.Time, processValidated bool) (models.WorkflowMutationResponse, error) {
	if _, err := s.EnsurePolicy(ctx, orgID); err != nil {
		return models.WorkflowMutationResponse{}, err
	}
	err := s.withImmediate(ctx, func(conn *sql.Conn) error {
		row, err := readWorkflowRow(ctx, conn, orgID, problemID)
		if err != nil {
			return err
		}
		if !row.WorkflowID.Valid || !row.StateID.Valid {
			return ErrUntracked
		}
		if row.StateID.String != from {
			return ErrStaleState
		}
		workflowID := row.WorkflowID.String
		stateID := row.StateID.String
		snapshot, err := s.readLatestPolicy(ctx, conn, orgID)
		if err != nil {
			return err
		}
		if snapshot.Revision != baseRevision {
			return &StaleRevisionError{Current: snapshot.Revision}
		}
		if _, ok := snapshot.Compiled.Workflow(workflowID); !ok {
			return ErrWorkflowRetired
		}
		if _, ok := snapshot.Compiled.State(workflowID, stateID); !ok {
			return ErrWorkflowRetired
		}
		trust, err := s.sourceTrust(ctx, conn, orgID, now, snapshot.Doc.TrustMinutes(), processValidated)
		if err != nil {
			return err
		}
		facts := gateFacts(row, comment, trust)
		edge, ok := snapshot.Compiled.Transition(workflowID, stateID, to)
		if !ok {
			return &InvalidTransitionError{Missing: []string{}, Allowed: permittedTargets(snapshot.Compiled, workflowID, stateID, facts)}
		}
		if !policy.RoleAtLeast(actorRole, edge.MinimumRole) {
			return ErrRoleRequired
		}
		if missing := policy.MissingGates(edge.Requires, facts); len(missing) > 0 {
			if row.Status == "recovered" && !trust.Trusted && containsGate(missing, "sourceRecovered") {
				return ErrSourceStateUntrusted
			}
			return &InvalidTransitionError{Missing: missing, Allowed: permittedTargets(snapshot.Compiled, workflowID, stateID, facts)}
		}
		nowText := now.UTC().Format(time.RFC3339)
		result, err := conn.ExecContext(ctx, `
			UPDATE problems SET state_id = ?, updated_at = ?
			WHERE org_id = ? AND id = ? AND workflow_id = ? AND state_id = ?`,
			to, nowText, orgID, problemID, workflowID, stateID)
		if err != nil {
			return fmt.Errorf("failed to transition workflow: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to confirm workflow transition: %w", err)
		}
		if affected != 1 {
			return ErrStaleState
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO events (id, org_id, problem_id, kind, author, created_at, body, policy_revision)
			VALUES (?, ?, ?, 'workflow_transition', ?, ?, ?, ?)`,
			uuid.New().String(), orgID, problemID, actor, nowText,
			fmt.Sprintf("Workflow %s moved from %s to %s", workflowID, stateID, to), snapshot.Revision); err != nil {
			return fmt.Errorf("failed to record workflow transition: %w", err)
		}
		if comment != "" {
			if _, err := conn.ExecContext(ctx, `
				INSERT INTO comments (id, org_id, problem_id, body, author, created_at)
				VALUES (?, ?, ?, ?, ?, ?)`,
				uuid.New().String(), orgID, problemID, comment, actor, nowText); err != nil {
				return fmt.Errorf("failed to store workflow transition comment: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return models.WorkflowMutationResponse{}, err
	}
	return s.workflowMutationResult(ctx, orgID, problemID, actorRole, now, processValidated)
}

func (s *Store) workflowMutationResult(ctx context.Context, orgID int64, problemID string, actorRole string, now time.Time, processValidated bool) (models.WorkflowMutationResponse, error) {
	problem, err := s.GetProblemByID(ctx, orgID, problemID)
	if err != nil {
		return models.WorkflowMutationResponse{}, err
	}
	status, err := s.WorkflowStatus(ctx, orgID, problemID, actorRole, now, processValidated)
	if err != nil {
		return models.WorkflowMutationResponse{}, err
	}
	tracked, ok := status.(*models.TrackedWorkflowStatus)
	if !ok {
		return models.WorkflowMutationResponse{}, errors.New("workflow mutation left episode untracked")
	}
	return models.WorkflowMutationResponse{Problem: problem, Workflow: tracked}, nil
}

func containsGate(gates []string, wanted string) bool {
	for _, gate := range gates {
		if gate == wanted {
			return true
		}
	}
	return false
}

// WorkflowStatus returns the untracked or tracked workflow view for one episode.
func (s *Store) WorkflowStatus(ctx context.Context, orgID int64, problemID string, actorRole string, now time.Time, processValidated bool) (any, error) {
	snapshot, err := s.ActivePolicy(ctx, orgID)
	if err != nil {
		return nil, err
	}
	problem, err := s.GetProblemByID(ctx, orgID, problemID)
	if err != nil {
		return nil, err
	}
	trust, err := s.sourceTrust(ctx, s.db, orgID, now, snapshot.Doc.TrustMinutes(), processValidated)
	if err != nil {
		return nil, err
	}
	if problem.WorkflowID == nil {
		level := snapshot.Compiled.ResolveLevel(problem.Labels[snapshot.Doc.SeverityKey]).Level.ID
		suggested, hasSelection := snapshot.Compiled.SelectWorkflow(problem.Labels, level)
		available := make([]models.WorkflowOption, 0, len(snapshot.Doc.Workflows))
		for _, workflow := range snapshot.Doc.Workflows {
			if hasSelection && workflow.ID != suggested {
				continue
			}
			available = append(available, models.WorkflowOption{
				ID:          workflow.ID,
				Name:        workflow.Name,
				MinimumRole: policy.EffectiveMinimumRole(workflow.MinimumRole),
			})
		}
		return models.UntrackedWorkflowStatus{
			Tracked:            false,
			PolicyRevision:     snapshot.Revision,
			AvailableWorkflows: available,
			SuggestedWorkflow:  suggested,
		}, nil
	}
	workflowID := *problem.WorkflowID
	stateID := ""
	if problem.StateID != nil {
		stateID = *problem.StateID
	}
	tracked := &models.TrackedWorkflowStatus{
		Tracked:      true,
		WorkflowID:   workflowID,
		StateID:      stateID,
		RevisionUsed: snapshot.Revision,
		Source: models.WorkflowSource{
			Status:      problem.Status,
			LastSuccess: trust.LastSuccess,
			Error:       trust.Error,
			Trusted:     trust.Trusted,
			Stale:       trust.Stale,
		},
		Edges: []models.WorkflowEdge{},
	}
	state, ok := snapshot.Compiled.State(workflowID, stateID)
	if !ok {
		tracked.Retired = true
		return tracked, nil
	}
	tracked.SourceConflict = problem.Status == "active" && state.OperatorComplete
	facts := gateFacts(&workflowRow{
		Status:         problem.Status,
		AcknowledgedBy: problem.AcknowledgedBy,
		Assignee:       problem.Assignee,
	}, "", trust)
	for _, edge := range snapshot.Compiled.TransitionsFrom(workflowID, stateID) {
		missing := policy.MissingGates(edge.Requires, facts)
		if !policy.RoleAtLeast(actorRole, edge.MinimumRole) {
			missing = append(missing, "role")
		}
		allowed := true
		for _, gate := range missing {
			// A comment is supplied with the transition request, so it never
			// disables the edge; the client asks for the comment text instead.
			if gate != "comment" {
				allowed = false
			}
		}
		tracked.Edges = append(tracked.Edges, models.WorkflowEdge{
			To:           edge.To,
			Allowed:      allowed,
			RequiredRole: policy.EffectiveMinimumRole(edge.MinimumRole),
			Missing:      missing,
		})
	}
	return tracked, nil
}
