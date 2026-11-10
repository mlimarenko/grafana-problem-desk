package models

import (
	"encoding/json"

	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

// Problem represents an alert incident episode scoped to an organization.
type Problem struct {
	ID             string            `json:"id"`
	Fingerprint    string            `json:"fingerprint"`
	StartsAt       string            `json:"startsAt"`
	EndsAt         string            `json:"endsAt"`
	Status         string            `json:"status"` // "active" | "recovered"
	Labels         map[string]string `json:"labels"`
	Annotations    map[string]string `json:"annotations"`
	GeneratorURL   string            `json:"generatorURL"`
	AcknowledgedBy string            `json:"acknowledgedBy"`
	AcknowledgedAt string            `json:"acknowledgedAt"`
	Assignee       string            `json:"assignee,omitempty"`
	LastSeenAt     string            `json:"lastSeenAt"`
	LevelID        string            `json:"levelId"`
	WorkflowID     *string           `json:"workflowId,omitempty"`
	StateID        *string           `json:"stateId,omitempty"`
}

// Comment represents user discussion on a problem episode.
type Comment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
}

// Event represents an audit event on a problem episode.
type Event struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Author         string `json:"author"`
	CreatedAt      string `json:"createdAt"`
	Body           string `json:"body"`
	PolicyRevision *int64 `json:"policyRevision,omitempty"`
}

// SourceStatus represents the sync state with Grafana Alertmanager.
type SourceStatus struct {
	LastSuccess string `json:"lastSuccess"`
	Error       string `json:"error"`
}

// ProblemsResponse represents the paginated problems listing response.
type ProblemsResponse struct {
	Problems       []Problem      `json:"problems"`
	Total          int            `json:"total"`
	Source         SourceStatus   `json:"source"`
	Summary        ProblemSummary `json:"summary"`
	Facets         ProblemFacets  `json:"facets"`
	Levels         []policy.Level `json:"levels"`
	SeverityKey    string         `json:"severityKey"`
	PolicyRevision int64          `json:"policyRevision"`
}

type ProblemSummary struct {
	Active         int            `json:"active"`
	Recovered      int            `json:"recovered"`
	Acknowledged   int            `json:"acknowledged"`
	Unacknowledged int            `json:"unacknowledged"`
	Unassigned     int            `json:"unassigned"`
	ByLevel        map[string]int `json:"byLevel"`
}

type ProblemFacets struct {
	Hosts     []string `json:"hosts"`
	Assignees []string `json:"assignees"`
	LabelKeys []string `json:"labelKeys"`
}

// ProblemDetailsResponse represents the problem details with comments and audit events.
type ProblemDetailsResponse struct {
	Problem  Problem   `json:"problem"`
	Comments []Comment `json:"comments"`
	Events   []Event   `json:"events"`
}

// CreateCommentRequest represents the payload for POST /problems/{id}/comments.
type CreateCommentRequest struct {
	Body string `json:"body"`
}

// AckRequest represents the payload for POST /problems/{id}/ack.
type AckRequest struct {
	Comment string `json:"comment,omitempty"`
}

// AssignRequest represents the payload for POST /problems/{id}/assign.
type AssignRequest struct {
	Assignee string `json:"assignee"`
}

// SyncResponse represents the response for POST /sync.
type SyncResponse struct {
	Success bool         `json:"success"`
	Error   string       `json:"error,omitempty"`
	Source  SourceStatus `json:"source"`
}

// AppConfig represents plugin instance JSONData configuration.
type AppConfig struct {
	GrafanaURL          string `json:"grafanaUrl"`
	PollIntervalSeconds int    `json:"pollIntervalSeconds"`
}

// SecureConfig represents plugin instance DecryptedSecureJSONData configuration.
type SecureConfig struct {
	GrafanaToken string `json:"grafanaToken"`
}

// OccupancyEntry counts tracked episodes pinned to a workflow state.
type OccupancyEntry struct {
	WorkflowID string `json:"workflowId"`
	StateID    string `json:"stateId"`
	Count      int    `json:"count"`
}

// PolicyResponse is the body of GET /policy.
type PolicyResponse struct {
	Revision        int64            `json:"revision"`
	Doc             *policy.Document `json:"doc"`
	DefaultsApplied bool             `json:"defaultsApplied"`
	Occupancy       []OccupancyEntry `json:"occupancy"`
}

// PutPolicyRequest is the body of PUT /policy.
type PutPolicyRequest struct {
	BaseRevision int64           `json:"baseRevision"`
	Doc          json.RawMessage `json:"doc"`
}

// PolicyPreviewRequest is the body of POST /policy/preview.
type PolicyPreviewRequest struct {
	Doc     json.RawMessage `json:"doc,omitempty"`
	Samples []PreviewSample `json:"samples"`
}

// PreviewSample is one classification sample for the policy preview.
type PreviewSample struct {
	Labels       map[string]string `json:"labels"`
	Acknowledged bool              `json:"acknowledged,omitempty"`
	Assignee     string            `json:"assignee,omitempty"`
	Status       string            `json:"status,omitempty"`
	Comment      string            `json:"comment,omitempty"`
}

// PreviewSampleResult is the effective classification of one sample.
type PreviewSampleResult struct {
	LevelID              string   `json:"levelId"`
	LevelMatched         bool     `json:"levelMatched"`
	WorkflowID           string   `json:"workflowId"`
	CanStart             bool     `json:"canStart"`
	MissingStartRequires []string `json:"missingStartRequires"`
}

// PolicyPreviewResponse is the body of POST /policy/preview.
type PolicyPreviewResponse struct {
	Valid               bool                  `json:"valid"`
	Issues              []policy.Issue        `json:"issues"`
	OccupancyViolations []OccupancyEntry      `json:"occupancyViolations"`
	Samples             []PreviewSampleResult `json:"samples"`
}

// PolicyRevisionSummary is one entry of the revision history.
type PolicyRevisionSummary struct {
	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	CreatedBy string `json:"createdBy"`
	Action    string `json:"action"`
}

// PolicyRevisionsResponse is the body of GET /policy/revisions.
type PolicyRevisionsResponse struct {
	Revisions []PolicyRevisionSummary `json:"revisions"`
}

// PolicyRevisionResponse is the body of GET /policy/revisions/{n}.
type PolicyRevisionResponse struct {
	Revision  int64            `json:"revision"`
	Doc       *policy.Document `json:"doc"`
	CreatedAt string           `json:"createdAt"`
	CreatedBy string           `json:"createdBy"`
}

// WorkflowOption is an available workflow in the untracked response.
type WorkflowOption struct {
	ID          string               `json:"id"`
	Name        policy.LocalizedText `json:"name"`
	MinimumRole string               `json:"minimumRole"`
}

// UntrackedWorkflowStatus describes an episode without a pinned workflow.
type UntrackedWorkflowStatus struct {
	Tracked            bool             `json:"tracked"`
	PolicyRevision     int64            `json:"policyRevision"`
	AvailableWorkflows []WorkflowOption `json:"availableWorkflows"`
	SuggestedWorkflow  string           `json:"suggestedWorkflow,omitempty"`
}

// WorkflowSource is the source-trust snapshot attached to a tracked episode.
type WorkflowSource struct {
	Status      string `json:"status"`
	LastSuccess string `json:"lastSuccess"`
	Error       string `json:"error"`
	Trusted     bool   `json:"trusted"`
	Stale       bool   `json:"stale"`
}

// WorkflowEdge describes one outgoing transition from the current state.
type WorkflowEdge struct {
	To           string   `json:"to"`
	Allowed      bool     `json:"allowed"`
	RequiredRole string   `json:"requiredRole"`
	Missing      []string `json:"missing"`
}

// TrackedWorkflowStatus describes an episode pinned to a workflow state.
type TrackedWorkflowStatus struct {
	Tracked        bool           `json:"tracked"`
	WorkflowID     string         `json:"workflowId"`
	StateID        string         `json:"stateId"`
	RevisionUsed   int64          `json:"revisionUsed"`
	Retired        bool           `json:"retired"`
	SourceConflict bool           `json:"sourceConflict"`
	Source         WorkflowSource `json:"source"`
	Edges          []WorkflowEdge `json:"edges"`
}

// StartWorkflowRequest is the body of POST /problems/{id}/workflow/start.
type StartWorkflowRequest struct {
	Workflow     string `json:"workflow,omitempty"`
	BaseRevision int64  `json:"baseRevision"`
	Comment      string `json:"comment,omitempty"`
}

// TransitionWorkflowRequest is the body of POST /problems/{id}/workflow/transition.
type TransitionWorkflowRequest struct {
	From         string `json:"from"`
	To           string `json:"to"`
	BaseRevision int64  `json:"baseRevision"`
	Comment      string `json:"comment,omitempty"`
}

// WorkflowMutationResponse is returned by start and transition.
type WorkflowMutationResponse struct {
	Problem  *Problem               `json:"problem"`
	Workflow *TrackedWorkflowStatus `json:"workflow"`
}
