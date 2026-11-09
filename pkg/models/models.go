package models

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
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
	Body      string `json:"body"`
}

// SourceStatus represents the sync state with Grafana Alertmanager.
type SourceStatus struct {
	LastSuccess string `json:"lastSuccess"`
	Error       string `json:"error"`
}

// ProblemsResponse represents the paginated problems listing response.
type ProblemsResponse struct {
	Problems []Problem    `json:"problems"`
	Total    int          `json:"total"`
	Source   SourceStatus `json:"source"`
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
