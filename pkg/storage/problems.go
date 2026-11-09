package storage

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
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

// ProblemQuery is validated by the resource handler before use.
type ProblemQuery struct {
	View, State, Severity, Host, Assignee, Assignment, Ack, Search, LabelKey, LabelValue string
	Sort, Order                                                                          string
	From, To, Now                                                                        time.Time
	Limit, Offset                                                                        int
}

const hostExpr = `COALESCE(NULLIF(json_extract(labels, '$.host'), ''), NULLIF(json_extract(labels, '$.hostname'), ''), NULLIF(json_extract(labels, '$.piping_host_name'), ''), NULLIF(json_extract(labels, '$.instance'), ''), '')`
const severityExpr = `CASE lower(COALESCE(json_extract(labels, '$.severity'), '')) WHEN 'disaster' THEN 6 WHEN 'critical' THEN 5 WHEN 'high' THEN 4 WHEN 'warning' THEN 3 WHEN 'average' THEN 3 WHEN 'info' THEN 2 WHEN 'information' THEN 2 ELSE 1 END`

// GetProblems reads the page, counts, and view facets from one SQLite snapshot.
func (s *Store) GetProblems(ctx context.Context, orgID int64, q ProblemQuery) (models.ProblemsResponse, error) {
	r := models.ProblemsResponse{Problems: []models.Problem{}, Facets: models.ProblemFacets{Hosts: []string{}, Assignees: []string{}, LabelKeys: []string{}}}
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
		add("json_extract(labels, '$.severity') = ?", q.Severity)
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
	countSQL := `SELECT count(*), COALESCE(sum(status='active'),0), COALESCE(sum(status='recovered'),0), COALESCE(sum(acknowledged_by<>''),0), COALESCE(sum(acknowledged_by=''),0), COALESCE(sum(assignee=''),0), COALESCE(sum(` + severityExpr + `>=4),0), COALESCE(sum(` + severityExpr + `=3),0) FROM problems WHERE ` + filter
	if err = tx.QueryRowContext(ctx, countSQL, args...).Scan(&r.Total, &r.Summary.Active, &r.Summary.Recovered, &r.Summary.Acknowledged, &r.Summary.Unacknowledged, &r.Summary.Unassigned, &r.Summary.Critical, &r.Summary.Warning); err != nil {
		return r, err
	}
	sorts := map[string]string{"severity": severityExpr, "started": "problemdesk_time_key(starts_at)", "recovered": "problemdesk_time_key(ends_at)", "duration": "(julianday(CASE WHEN status='active' THEN ? ELSE ends_at END) - julianday(starts_at))"}
	sortExpr, ok := sorts[q.Sort]
	if !ok || (q.Order != "asc" && q.Order != "desc") {
		return r, fmt.Errorf("invalid sort")
	}
	selectArgs := append([]any{}, args...)
	if q.Sort == "duration" {
		selectArgs = append(selectArgs, stamp(q.Now))
	}
	selectArgs = append(selectArgs, q.Limit, q.Offset)
	rows, err := tx.QueryContext(ctx, `SELECT id, fingerprint, starts_at, ends_at, status, labels, annotations, generator_url, acknowledged_by, acknowledged_at, assignee, last_seen_at FROM problems WHERE `+filter+` ORDER BY `+sortExpr+` `+strings.ToUpper(q.Order)+`, id `+strings.ToUpper(q.Order)+` LIMIT ? OFFSET ?`, selectArgs...)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var p models.Problem
		var labels, annotations string
		if err = rows.Scan(&p.ID, &p.Fingerprint, &p.StartsAt, &p.EndsAt, &p.Status, &labels, &annotations, &p.GeneratorURL, &p.AcknowledgedBy, &p.AcknowledgedAt, &p.Assignee, &p.LastSeenAt); err != nil {
			rows.Close()
			return r, err
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
