package plugin

import (
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/storage"
)

func parseProblemInstant(raw string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, err
	}
	t = t.UTC()
	if t.Year() < 0 || t.Year() > 9999 {
		return time.Time{}, errors.New("timestamp outside RFC3339 UTC range")
	}
	return t, nil
}

func parseProblemQuery(values url.Values, now time.Time) (storage.ProblemQuery, error) {
	invalid := errors.New("invalid query")
	q := storage.ProblemQuery{View: "problems", State: "all", Assignment: "all", Ack: "all", Sort: "severity", Order: "desc", Limit: 50, Now: now}
	for _, v := range values {
		if len(v) != 1 || len(v[0]) > 1024 {
			return q, invalid
		}
	}
	enum := func(key string, target *string, allowed ...string) bool {
		v, exists := values[key]
		if !exists {
			return true
		}
		for _, a := range allowed {
			if v[0] == a {
				*target = a
				return true
			}
		}
		return false
	}
	if !enum("view", &q.View, "problems", "recent", "history") || !enum("state", &q.State, "all", "active", "recovered") || !enum("assignment", &q.Assignment, "all", "assigned", "unassigned") || !enum("ack", &q.Ack, "all", "acknowledged", "unacknowledged") || !enum("sort", &q.Sort, "severity", "started", "duration", "recovered") || !enum("order", &q.Order, "asc", "desc") {
		return q, invalid
	}
	q.Severity, q.WorkState, q.Workflow = values.Get("severity"), values.Get("workState"), values.Get("workflow")
	q.Host, q.Assignee, q.Search = values.Get("host"), values.Get("assignee"), values.Get("search")
	q.LabelKey, q.LabelValue = values.Get("labelKey"), values.Get("labelValue")
	_, hasKey := values["labelKey"]
	_, hasValue := values["labelValue"]
	if hasKey != hasValue || (hasKey && q.LabelKey == "") || len(q.Severity) > 256 || len(q.WorkState) > 256 || len(q.Workflow) > 256 || len(q.Host) > 256 || len(q.Assignee) > 256 || len(q.LabelKey) > 256 || len(q.LabelValue) > 1024 {
		return q, invalid
	}
	if v, ok := values["limit"]; ok {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 || n > 100 {
			return q, invalid
		}
		q.Limit = n
	}
	if v, ok := values["offset"]; ok {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 0 {
			return q, invalid
		}
		q.Offset = n
	}
	_, hasFrom := values["from"]
	_, hasTo := values["to"]
	if q.View == "problems" {
		if hasFrom || hasTo || q.State == "recovered" {
			return q, invalid
		}
		return q, nil
	}
	if q.View == "history" && hasFrom != hasTo {
		return q, invalid
	}
	q.To = now
	if q.View == "recent" {
		q.From = now.Add(-24 * time.Hour)
	} else {
		q.From = now.Add(-7 * 24 * time.Hour)
	}
	if hasFrom {
		t, err := parseProblemInstant(values.Get("from"))
		if err != nil {
			return q, invalid
		}
		q.From = t
	}
	if hasTo {
		t, err := parseProblemInstant(values.Get("to"))
		if err != nil {
			return q, invalid
		}
		q.To = t
	}
	if q.From.After(q.To) || (q.View == "history" && q.To.Sub(q.From) > 366*24*time.Hour) {
		return q, invalid
	}
	return q, nil
}
