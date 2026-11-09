package storage

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

var ErrAmbiguous = errors.New("multiple matching problems")

// ResolveActiveProblem resolves a unique active episode by a subset of labels.
func (s *Store) ResolveActiveProblem(ctx context.Context, orgID int64, matchers map[string]string) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, labels FROM problems WHERE org_id = ? AND status = 'active'`, orgID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var id string
	for rows.Next() {
		var candidate, rawLabels string
		if err := rows.Scan(&candidate, &rawLabels); err != nil {
			return "", err
		}
		var labels map[string]string
		if err := json.Unmarshal([]byte(rawLabels), &labels); err != nil {
			return "", err
		}
		matches := true
		for key, value := range matchers {
			if actual, ok := labels[key]; !ok || actual != value {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if id != "" {
			return "", ErrAmbiguous
		}
		id = candidate
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if id == "" {
		return "", ErrNotFound
	}
	return id, nil
}

// ResolveProblem returns only the exact observed occurrence for this organization.
func (s *Store) ResolveProblem(ctx context.Context, orgID int64, fingerprint string, labels map[string]string, startsAt time.Time) (string, error) {
	query := `SELECT id, starts_at, labels FROM problems WHERE org_id = ?`
	args := []any{orgID}
	if fingerprint != "" {
		query += ` AND fingerprint = ?`
		args = append(args, fingerprint)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	id := ""
	for rows.Next() {
		var candidate, start, rawLabels string
		if err := rows.Scan(&candidate, &start, &rawLabels); err != nil {
			return "", err
		}
		t, err := time.Parse(time.RFC3339Nano, start)
		if err != nil {
			return "", err
		}
		if !t.Equal(startsAt) {
			continue
		}
		if fingerprint == "" {
			var stored map[string]string
			if err := json.Unmarshal([]byte(rawLabels), &stored); err != nil {
				return "", err
			}
			if !reflect.DeepEqual(stored, labels) {
				continue
			}
		}
		if id != "" {
			return "", ErrAmbiguous
		}
		id = candidate
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if id == "" {
		return "", ErrNotFound
	}
	return id, nil
}
