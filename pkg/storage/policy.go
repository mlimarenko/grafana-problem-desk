package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
)

const (
	policyBootstrapActor  = "system"
	policyActionBootstrap = "bootstrap"
	policyActionUpdate    = "update"
	policyCacheLimit      = 256
)

var (
	// ErrPolicyRevisionNotFound is returned for an unknown organization revision.
	ErrPolicyRevisionNotFound = errors.New("policy revision not found")

	errNoPolicy = errors.New("organization has no policy revision")
)

// PolicyStaleError reports an optimistic-concurrency mismatch on PUT /policy.
type PolicyStaleError struct {
	Current int64
}

func (e *PolicyStaleError) Error() string {
	return fmt.Sprintf("stale policy revision: current is %d", e.Current)
}

// policySnapshot is one immutable, compiled organization policy revision.
type policySnapshot struct {
	Revision  int64
	Doc       *policy.Document
	Canonical string
	CreatedAt string
	CreatedBy string
	Action    string
	Compiled  *policy.Compiled
}

// dbQueryer is implemented by both *sql.DB and *sql.Conn.
type dbQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// withImmediate runs fn inside BEGIN IMMEDIATE on one connection. Taking the
// write lock up front avoids a deferred upgrade failing mid-transaction.
func (s *Store) withImmediate(ctx context.Context, fn func(conn *sql.Conn) error) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
		_ = conn.Close()
	}()
	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

func policyCacheKey(orgID, revision int64) string {
	// The organization is part of the key by contract: revision numbers are per org.
	return strconv.FormatInt(orgID, 10) + ":" + strconv.FormatInt(revision, 10)
}

func (s *Store) cachedPolicy(key string) *policySnapshot {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	return s.policyCache[key]
}

func (s *Store) storePolicySnapshot(key string, snapshot *policySnapshot) {
	s.policyMu.Lock()
	defer s.policyMu.Unlock()
	if s.policyCache == nil || len(s.policyCache) >= policyCacheLimit {
		s.policyCache = make(map[string]*policySnapshot)
	}
	s.policyCache[key] = snapshot
}

// readLatestPolicy loads the newest revision for an organization through the
// given handle and compiles it (or reuses the org:revision cache entry).
func (s *Store) readLatestPolicy(ctx context.Context, queryer dbQueryer, orgID int64) (*policySnapshot, error) {
	var revision int64
	var docJSON, createdAt, createdBy, action string
	err := queryer.QueryRowContext(ctx, `
		SELECT r.revision, r.doc_json, r.created_at, r.created_by,
		       COALESCE((SELECT a.action FROM policy_audit a
		                 WHERE a.org_id = r.org_id AND a.revision = r.revision
		                 ORDER BY a.id DESC LIMIT 1), '')
		FROM policy_revisions r
		WHERE r.org_id = ?
		ORDER BY r.revision DESC
		LIMIT 1`, orgID).Scan(&revision, &docJSON, &createdAt, &createdBy, &action)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoPolicy
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read policy revision: %w", err)
	}
	key := policyCacheKey(orgID, revision)
	if cached := s.cachedPolicy(key); cached != nil {
		return cached, nil
	}
	doc, issues := policy.ParseDocument([]byte(docJSON))
	if len(issues) > 0 {
		return nil, &policy.ValidationError{Issues: issues}
	}
	compiled, err := policy.Compile(doc, revision)
	if err != nil {
		return nil, err
	}
	snapshot := &policySnapshot{
		Revision:  revision,
		Doc:       doc,
		Canonical: docJSON,
		CreatedAt: createdAt,
		CreatedBy: createdBy,
		Action:    action,
		Compiled:  compiled,
	}
	s.storePolicySnapshot(key, snapshot)
	return snapshot, nil
}

// EnsurePolicy persists the built-in default as immutable revision 1 for an
// organization that has no policy yet. It is a system initialization, not an
// operator edit.
func (s *Store) EnsurePolicy(ctx context.Context, orgID int64) (*policySnapshot, error) {
	err := s.withImmediate(ctx, func(conn *sql.Conn) error {
		var existing int64
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM policy_revisions WHERE org_id = ?`, orgID).Scan(&existing); err != nil {
			return fmt.Errorf("failed to inspect policy revisions: %w", err)
		}
		if existing > 0 {
			return nil
		}
		doc := policy.DefaultDoc()
		canonical, err := policy.CanonicalJSON(doc)
		if err != nil {
			return fmt.Errorf("failed to encode default policy: %w", err)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO policy_revisions (org_id, revision, doc_json, created_at, created_by)
			VALUES (?, 1, ?, ?, ?)`, orgID, string(canonical), now, policyBootstrapActor); err != nil {
			return fmt.Errorf("failed to persist default policy: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO policy_audit (org_id, revision, actor, created_at, action)
			VALUES (?, 1, ?, ?, ?)`, orgID, policyBootstrapActor, now, policyActionBootstrap); err != nil {
			return fmt.Errorf("failed to audit default policy: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.readLatestPolicy(ctx, s.db, orgID)
}

// ActivePolicy returns the newest compiled revision, bootstrapping revision 1.
func (s *Store) ActivePolicy(ctx context.Context, orgID int64) (*policySnapshot, error) {
	snapshot, err := s.readLatestPolicy(ctx, s.db, orgID)
	if errors.Is(err, errNoPolicy) {
		return s.EnsurePolicy(ctx, orgID)
	}
	return snapshot, err
}

// GetPolicy returns the active document with occupancy and default-origin state.
func (s *Store) GetPolicy(ctx context.Context, orgID int64) (models.PolicyResponse, error) {
	snapshot, err := s.ActivePolicy(ctx, orgID)
	if err != nil {
		return models.PolicyResponse{}, err
	}
	occupancy, err := s.PolicyOccupancy(ctx, orgID)
	if err != nil {
		return models.PolicyResponse{}, err
	}
	return models.PolicyResponse{
		Revision:        snapshot.Revision,
		Doc:             snapshot.Doc,
		DefaultsApplied: snapshot.Revision == 1 && snapshot.Action == policyActionBootstrap,
		Occupancy:       occupancy,
	}, nil
}

// PolicyOccupancy counts episodes pinned to each workflow state.
func (s *Store) PolicyOccupancy(ctx context.Context, orgID int64) ([]models.OccupancyEntry, error) {
	return occupancyQuery(ctx, s.db, orgID)
}

func occupancyQuery(ctx context.Context, queryer dbQueryer, orgID int64) ([]models.OccupancyEntry, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT workflow_id, state_id, COUNT(*)
		FROM problems
		WHERE org_id = ? AND workflow_id IS NOT NULL AND state_id IS NOT NULL
		GROUP BY workflow_id, state_id
		ORDER BY workflow_id, state_id`, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow occupancy: %w", err)
	}
	defer rows.Close()
	entries := make([]models.OccupancyEntry, 0)
	for rows.Next() {
		var entry models.OccupancyEntry
		if err := rows.Scan(&entry.WorkflowID, &entry.StateID, &entry.Count); err != nil {
			return nil, fmt.Errorf("failed to scan workflow occupancy: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// PutPolicy validates and appends a new immutable revision under optimistic CAS.
// Any stale or occupancy failure writes nothing.
func (s *Store) PutPolicy(ctx context.Context, orgID int64, actor string, baseRevision int64, doc *policy.Document) (int64, error) {
	if doc == nil {
		return 0, &policy.ValidationError{Issues: []policy.Issue{{Path: "$.doc", Code: "required", Message: "policy document is required"}}}
	}
	policy.Normalize(doc)
	if issues := policy.Validate(doc); len(issues) > 0 {
		return 0, &policy.ValidationError{Issues: issues}
	}
	canonical, err := policy.CanonicalJSON(doc)
	if err != nil {
		return 0, fmt.Errorf("failed to encode policy: %w", err)
	}
	if _, err := s.EnsurePolicy(ctx, orgID); err != nil {
		return 0, err
	}

	var newRevision int64
	err = s.withImmediate(ctx, func(conn *sql.Conn) error {
		var current int64
		if err := conn.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) FROM policy_revisions WHERE org_id = ?`, orgID).Scan(&current); err != nil {
			return fmt.Errorf("failed to read current policy revision: %w", err)
		}
		if current != baseRevision {
			return &PolicyStaleError{Current: current}
		}
		occupancy, err := occupancyQuery(ctx, conn, orgID)
		if err != nil {
			return err
		}
		if issues := occupancyIssues(doc, occupancy); len(issues) > 0 {
			return &policy.ValidationError{Issues: issues}
		}
		newRevision = current + 1
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO policy_revisions (org_id, revision, doc_json, created_at, created_by)
			VALUES (?, ?, ?, ?, ?)`, orgID, newRevision, string(canonical), now, actor); err != nil {
			return fmt.Errorf("failed to persist policy revision: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO policy_audit (org_id, revision, actor, created_at, action)
			VALUES (?, ?, ?, ?, ?)`, orgID, newRevision, actor, now, policyActionUpdate); err != nil {
			return fmt.Errorf("failed to audit policy revision: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newRevision, nil
}

// occupancyViolation records one occupancy entry a candidate document invalidates.
type occupancyViolation struct {
	Entry           models.OccupancyEntry
	WorkflowRemoved bool
	WorkflowIndex   int
}

func policyOccupancyViolations(doc *policy.Document, occupancy []models.OccupancyEntry) []occupancyViolation {
	workflowIndex := make(map[string]int, len(doc.Workflows))
	workflowByID := make(map[string]*policy.Workflow, len(doc.Workflows))
	for i := range doc.Workflows {
		workflowIndex[doc.Workflows[i].ID] = i
		workflowByID[doc.Workflows[i].ID] = &doc.Workflows[i]
	}
	violations := make([]occupancyViolation, 0)
	for _, entry := range occupancy {
		if entry.WorkflowID == "" || entry.StateID == "" {
			continue
		}
		workflow, ok := workflowByID[entry.WorkflowID]
		if !ok {
			violations = append(violations, occupancyViolation{Entry: entry, WorkflowRemoved: true})
			continue
		}
		hasState := false
		for _, state := range workflow.States {
			if state.ID == entry.StateID {
				hasState = true
				break
			}
		}
		if !hasState {
			violations = append(violations, occupancyViolation{Entry: entry, WorkflowIndex: workflowIndex[entry.WorkflowID]})
		}
	}
	return violations
}

// PolicyOccupancyViolations lists pinned episodes a candidate document would strand.
func PolicyOccupancyViolations(doc *policy.Document, occupancy []models.OccupancyEntry) []models.OccupancyEntry {
	violations := policyOccupancyViolations(doc, occupancy)
	entries := make([]models.OccupancyEntry, 0, len(violations))
	for _, violation := range violations {
		entries = append(entries, violation.Entry)
	}
	return entries
}

// occupancyIssues refuses document edits that would make tracked episodes
// uninterpretable: a workflow or state with pinned episodes cannot disappear.
func occupancyIssues(doc *policy.Document, occupancy []models.OccupancyEntry) []policy.Issue {
	removedWorkflows := make(map[string]int)
	removedStates := make(map[string]int)
	stateIndex := make(map[string]int)
	workflowOrder := make([]string, 0)
	stateOrder := make([]string, 0)
	for _, violation := range policyOccupancyViolations(doc, occupancy) {
		entry := violation.Entry
		if violation.WorkflowRemoved {
			if _, seen := removedWorkflows[entry.WorkflowID]; !seen {
				workflowOrder = append(workflowOrder, entry.WorkflowID)
			}
			removedWorkflows[entry.WorkflowID] += entry.Count
			continue
		}
		key := entry.WorkflowID + "\x00" + entry.StateID
		if _, seen := removedStates[key]; !seen {
			stateOrder = append(stateOrder, key)
			stateIndex[key] = violation.WorkflowIndex
		}
		removedStates[key] += entry.Count
	}
	issues := make([]policy.Issue, 0, len(workflowOrder)+len(stateOrder))
	for _, workflowID := range workflowOrder {
		issues = append(issues, policy.Issue{
			Path:    "$.workflows",
			Code:    "immutable",
			Message: fmt.Sprintf("workflow %q is still used by %d tracked episode(s)", workflowID, removedWorkflows[workflowID]),
		})
	}
	for _, key := range stateOrder {
		parts := strings.SplitN(key, "\x00", 2)
		issues = append(issues, policy.Issue{
			Path:    fmt.Sprintf("$.workflows[%d].states", stateIndex[key]),
			Code:    "immutable",
			Message: fmt.Sprintf("state %q of workflow %q is still used by %d tracked episode(s)", parts[1], parts[0], removedStates[key]),
		})
	}
	return issues
}

// SourceTrustFor evaluates source trust against an explicit trust window.
func (s *Store) SourceTrustFor(ctx context.Context, orgID int64, now time.Time, trustMinutes int, processValidated bool) (models.WorkflowSource, error) {
	trust, err := s.sourceTrust(ctx, s.db, orgID, now, trustMinutes, processValidated)
	if err != nil {
		return models.WorkflowSource{}, err
	}
	return models.WorkflowSource{
		LastSuccess: trust.LastSuccess,
		Error:       trust.Error,
		Trusted:     trust.Trusted,
		Stale:       trust.Stale,
	}, nil
}

// PolicyRevisions returns the newest revisions first, bounded for the API.
func (s *Store) PolicyRevisions(ctx context.Context, orgID int64, limit int) ([]models.PolicyRevisionSummary, error) {
	if _, err := s.EnsurePolicy(ctx, orgID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.revision, r.created_at, r.created_by,
		       COALESCE((SELECT a.action FROM policy_audit a
		                 WHERE a.org_id = r.org_id AND a.revision = r.revision
		                 ORDER BY a.id DESC LIMIT 1), 'update')
		FROM policy_revisions r
		WHERE r.org_id = ?
		ORDER BY r.revision DESC
		LIMIT ?`, orgID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list policy revisions: %w", err)
	}
	defer rows.Close()
	revisions := make([]models.PolicyRevisionSummary, 0)
	for rows.Next() {
		var summary models.PolicyRevisionSummary
		if err := rows.Scan(&summary.Revision, &summary.CreatedAt, &summary.CreatedBy, &summary.Action); err != nil {
			return nil, fmt.Errorf("failed to scan policy revision: %w", err)
		}
		revisions = append(revisions, summary)
	}
	return revisions, rows.Err()
}

// PolicyRevision returns one immutable revision for the read-only history.
func (s *Store) PolicyRevision(ctx context.Context, orgID, revision int64) (models.PolicyRevisionResponse, error) {
	var docJSON, createdAt, createdBy string
	err := s.db.QueryRowContext(ctx, `
		SELECT doc_json, created_at, created_by
		FROM policy_revisions
		WHERE org_id = ? AND revision = ?`, orgID, revision).Scan(&docJSON, &createdAt, &createdBy)
	if errors.Is(err, sql.ErrNoRows) {
		return models.PolicyRevisionResponse{}, ErrPolicyRevisionNotFound
	}
	if err != nil {
		return models.PolicyRevisionResponse{}, fmt.Errorf("failed to read policy revision: %w", err)
	}
	doc, issues := policy.ParseDocument([]byte(docJSON))
	if len(issues) > 0 {
		return models.PolicyRevisionResponse{}, &policy.ValidationError{Issues: issues}
	}
	return models.PolicyRevisionResponse{
		Revision:  revision,
		Doc:       doc,
		CreatedAt: createdAt,
		CreatedBy: createdBy,
	}, nil
}
