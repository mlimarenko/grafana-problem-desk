package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
	"github.com/mlimarenko/grafana-problem-desk/pkg/policy"
	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("problemdesk_contains", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		text, okText := args[0].(string)
		query, okQuery := args[1].(string)
		if !okText || !okQuery {
			return nil, fmt.Errorf("invalid search value")
		}
		if strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
			return int64(1), nil
		}
		return int64(0), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("problemdesk_time_key", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		raw, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("invalid timestamp value")
		}
		if raw == "" {
			return nil, nil
		}
		instant, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, err
		}
		instant = instant.UTC()
		if instant.Year() < 0 || instant.Year() > 9999 {
			return nil, fmt.Errorf("timestamp out of range")
		}
		return instant.Format("2006-01-02T15:04:05.000000000Z"), nil
	})
}

// ProblemQuery is validated by the resource handler before use. Severity is the
// configured level-id filter; WorkState and Workflow select tracked rows only.
type ProblemQuery struct {
	View, State, Severity, WorkState, Workflow, Host, Assignee, Assignment, Ack, Search, LabelKey, LabelValue string
	Sort, Order                                                                                               string
	From, To, Now                                                                                             time.Time
	Limit, Offset                                                                                             int
}

const hostExpr = `COALESCE(NULLIF(json_extract(labels, '$.host'), ''), NULLIF(json_extract(labels, '$.hostname'), ''), NULLIF(json_extract(labels, '$.piping_host_name'), ''), NULLIF(json_extract(labels, '$.instance'), ''), '')`

// levelRankCase builds a parameterized rank expression for the active levels.
// Retired level ids fall below every configured level.
func levelRankCase(compiled *policy.Compiled) (string, []any) {
	var builder strings.Builder
	builder.WriteString("CASE level_id")
	args := make([]any, 0, len(compiled.Doc.Levels)*2)
	for _, level := range compiled.Doc.Levels {
		builder.WriteString(" WHEN ? THEN ?")
		args = append(args, level.ID, level.Rank)
	}
	builder.WriteString(" ELSE 0 END")
	return builder.String(), args
}

// GetProblems reads the page, counts, level summary and view facets from one SQLite snapshot.
func (s *Store) GetProblems(ctx context.Context, orgID int64, q ProblemQuery) (models.ProblemsResponse, error) {
	snapshot, err := s.ActivePolicy(ctx, orgID)
	if err != nil {
		return models.ProblemsResponse{}, err
	}
	r := models.ProblemsResponse{
		Problems:       []models.Problem{},
		Facets:         models.ProblemFacets{Hosts: []string{}, Assignees: []string{}, LabelKeys: []string{}},
		Levels:         snapshot.Doc.Levels,
		SeverityKey:    snapshot.Doc.SeverityKey,
		PolicyRevision: snapshot.Revision,
		Summary:        models.ProblemSummary{ByLevel: map[string]int{}},
	}
	for _, level := range snapshot.Doc.Levels {
		r.Summary.ByLevel[level.ID] = 0
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	base := []string{"org_id = ?"}
	baseArgs := []any{orgID}
	stamp := func(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
	switch q.View {
	case "problems":
		base = append(base, "status = 'active'")
	case "recent":
		base = append(base, "(status = 'active' OR (status = 'recovered' AND problemdesk_time_key(ends_at) >= problemdesk_time_key(?) AND problemdesk_time_key(ends_at) <= problemdesk_time_key(?)))")
		baseArgs = append(baseArgs, stamp(q.From), stamp(q.To))
	case "history":
		base = append(base, "problemdesk_time_key(starts_at) <= problemdesk_time_key(?) AND (status = 'active' OR problemdesk_time_key(ends_at) >= problemdesk_time_key(?))")
		baseArgs = append(baseArgs, stamp(q.To), stamp(q.From))
	default:
		return r, fmt.Errorf("invalid view")
	}
	where := append([]string{}, base...)
	args := append([]any{}, baseArgs...)
	add := func(clause string, values ...any) { where = append(where, clause); args = append(args, values...) }
	if q.State == "active" || q.State == "recovered" {
		add("status = ?", q.State)
	}
	if q.Severity != "" {
		add("level_id = ?", q.Severity)
	}
	if q.Workflow != "" {
		add("workflow_id IS NOT NULL AND workflow_id = ?", q.Workflow)
	}
	if q.WorkState != "" {
		add("state_id IS NOT NULL AND state_id = ?", q.WorkState)
	}
	if q.Host != "" {
		add(`(problemdesk_contains(COALESCE(json_extract(labels, '$.host'), ''), ?) OR problemdesk_contains(COALESCE(json_extract(labels, '$.hostname'), ''), ?) OR problemdesk_contains(COALESCE(json_extract(labels, '$.piping_host_name'), ''), ?) OR problemdesk_contains(COALESCE(json_extract(labels, '$.instance'), ''), ?))`, q.Host, q.Host, q.Host, q.Host)
	}
	if q.Assignee != "" {
		add("assignee = ?", q.Assignee)
	}
	if q.Assignment == "assigned" {
		add("assignee <> ''")
	}
	if q.Assignment == "unassigned" {
		add("assignee = ''")
	}
	if q.Ack == "acknowledged" {
		add("acknowledged_by <> ''")
	}
	if q.Ack == "unacknowledged" {
		add("acknowledged_by = ''")
	}
	if q.Search != "" {
		add(`(problemdesk_contains(fingerprint, ?) OR EXISTS (SELECT 1 FROM json_each(problems.labels) WHERE problemdesk_contains(COALESCE(key, ''), ?) OR problemdesk_contains(COALESCE(value, ''), ?)) OR EXISTS (SELECT 1 FROM json_each(problems.annotations) WHERE problemdesk_contains(COALESCE(key, ''), ?) OR problemdesk_contains(COALESCE(value, ''), ?)))`, q.Search, q.Search, q.Search, q.Search, q.Search)
	}
	if q.LabelKey != "" {
		add("EXISTS (SELECT 1 FROM json_each(problems.labels) WHERE key = ? AND value = ?)", q.LabelKey, q.LabelValue)
	}
	filter := strings.Join(where, " AND ")
	countSQL := `SELECT count(*), COALESCE(sum(status='active'),0), COALESCE(sum(status='recovered'),0), COALESCE(sum(acknowledged_by<>''),0), COALESCE(sum(acknowledged_by=''),0), COALESCE(sum(assignee=''),0) FROM problems WHERE ` + filter
	if err = tx.QueryRowContext(ctx, countSQL, args...).Scan(&r.Total, &r.Summary.Active, &r.Summary.Recovered, &r.Summary.Acknowledged, &r.Summary.Unacknowledged, &r.Summary.Unassigned); err != nil {
		return r, err
	}

	// Level summary spans the whole filtered selection, retired ids included.
	byLevelSQL := `SELECT level_id, count(*) FROM problems WHERE ` + filter + ` GROUP BY level_id`
	levelRows, err := tx.QueryContext(ctx, byLevelSQL, args...)
	if err != nil {
		return r, err
	}
	for levelRows.Next() {
		var levelID string
		var count int
		if err = levelRows.Scan(&levelID, &count); err != nil {
			levelRows.Close()
			return r, err
		}
		r.Summary.ByLevel[levelID] += count
	}
	if err = levelRows.Err(); err != nil {
		levelRows.Close()
		return r, err
	}
	if err = levelRows.Close(); err != nil {
		return r, err
	}

	rankExpr, rankArgs := levelRankCase(snapshot.Compiled)
	sorts := map[string]string{"severity": rankExpr, "started": "problemdesk_time_key(starts_at)", "recovered": "problemdesk_time_key(ends_at)", "duration": "(julianday(CASE WHEN status='active' THEN ? ELSE ends_at END) - julianday(starts_at))"}
	sortExpr, ok := sorts[q.Sort]
	if !ok || (q.Order != "asc" && q.Order != "desc") {
		return r, fmt.Errorf("invalid sort")
	}
	direction := strings.ToUpper(q.Order)
	orderClause := sortExpr + " " + direction + ", id " + direction
	if q.Sort == "severity" {
		orderClause = rankExpr + " " + direction + ", level_id ASC, id " + direction
	}
	selectArgs := append([]any{}, args...)
	if q.Sort == "duration" {
		selectArgs = append(selectArgs, stamp(q.Now))
	}
	if q.Sort == "severity" {
		selectArgs = append(selectArgs, rankArgs...)
	}
	selectArgs = append(selectArgs, q.Limit, q.Offset)
	rows, err := tx.QueryContext(ctx, `SELECT id, fingerprint, starts_at, ends_at, status, labels, annotations, generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at, level_id, workflow_id, state_id FROM problems WHERE `+filter+` ORDER BY `+orderClause+` LIMIT ? OFFSET ?`, selectArgs...)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var p models.Problem
		var labels, annotations string
		var workflowID, stateID sql.NullString
		if err = rows.Scan(&p.ID, &p.Fingerprint, &p.StartsAt, &p.EndsAt, &p.Status, &labels, &annotations, &p.GeneratorURL, &p.AcknowledgedBy, &p.AcknowledgedAt, &p.Assignee, &p.LastSeenAt, &p.LevelID, &workflowID, &stateID); err != nil {
			rows.Close()
			return r, err
		}
		if workflowID.Valid {
			value := workflowID.String
			p.WorkflowID = &value
		}
		if stateID.Valid {
			value := stateID.String
			p.StateID = &value
		}
		if err = json.Unmarshal([]byte(labels), &p.Labels); err != nil {
			rows.Close()
			return r, err
		}
		if err = json.Unmarshal([]byte(annotations), &p.Annotations); err != nil {
			rows.Close()
			return r, err
		}
		if p.Labels == nil {
			p.Labels = map[string]string{}
		}
		if p.Annotations == nil {
			p.Annotations = map[string]string{}
		}
		r.Problems = append(r.Problems, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return r, err
	}
	if err = rows.Close(); err != nil {
		return r, err
	}
	baseSQL := strings.Join(base, " AND ")
	queries := []struct {
		sql  string
		dest *[]string
	}{
		{`SELECT DISTINCT ` + hostExpr + ` AS value FROM problems WHERE ` + baseSQL + ` AND ` + hostExpr + ` <> '' ORDER BY value LIMIT 200`, &r.Facets.Hosts},
		{`SELECT DISTINCT assignee AS value FROM problems WHERE ` + baseSQL + ` AND assignee <> '' ORDER BY value LIMIT 200`, &r.Facets.Assignees},
		{`SELECT DISTINCT json_each.key AS value FROM problems, json_each(problems.labels) WHERE ` + baseSQL + ` AND json_each.key IS NOT NULL ORDER BY value LIMIT 200`, &r.Facets.LabelKeys},
	}
	for _, query := range queries {
		rows, err = tx.QueryContext(ctx, query.sql, baseArgs...)
		if err != nil {
			return r, err
		}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				rows.Close()
				return r, err
			}
			*query.dest = append(*query.dest, v)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return r, err
		}
		if err = rows.Close(); err != nil {
			return r, err
		}
	}
	if err = tx.Commit(); err != nil {
		return r, err
	}
	return r, nil
}
