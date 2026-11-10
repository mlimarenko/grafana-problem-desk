package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mlimarenko/grafana-problem-desk/pkg/models"
)

const assigneeSchema = `
CREATE TABLE assignee_catalogs (org_id INTEGER PRIMARY KEY, revision INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL);
CREATE TABLE assignee_members (org_id INTEGER NOT NULL, uid TEXT NOT NULL, login TEXT NOT NULL, name TEXT NOT NULL, PRIMARY KEY(org_id, uid), UNIQUE(org_id, login));
`

var ErrAssigneeUnavailable = errors.New("assignee unavailable")
var ErrAssigneeStale = errors.New("stale assignee revision")

func (s *Store) GetAssignees(ctx context.Context, orgID int64) (models.AssigneeCatalog, error) {
	c := models.AssigneeCatalog{Members: []models.AssigneeMember{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT revision, updated_at, updated_by FROM assignee_catalogs WHERE org_id = ?`, orgID).Scan(&c.Revision, &c.UpdatedAt, &c.UpdatedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return c, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT uid, login, name FROM assignee_members WHERE org_id = ? ORDER BY login`, orgID)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var m models.AssigneeMember
		if err := rows.Scan(&m.UID, &m.Login, &m.Name); err != nil {
			rows.Close()
			return c, err
		}
		c.Members = append(c.Members, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return c, err
	}
	if err := rows.Close(); err != nil {
		return c, err
	}
	return c, tx.Commit()
}

func (s *Store) PutAssignees(ctx context.Context, orgID int64, actor string, base int64, members []models.AssigneeMember) (models.AssigneeCatalog, error) {
	c := models.AssigneeCatalog{}
	err := s.withImmediate(ctx, func(tx *sql.Conn) error {
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM assignee_catalogs WHERE org_id = ?`, orgID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision != base {
			return ErrAssigneeStale
		}
		c = models.AssigneeCatalog{Revision: revision + 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedBy: actor, Members: members}
		if c.Members == nil {
			c.Members = []models.AssigneeMember{}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO assignee_catalogs (org_id, revision, updated_at, updated_by) VALUES (?, ?, ?, ?) ON CONFLICT(org_id) DO UPDATE SET revision=excluded.revision, updated_at=excluded.updated_at, updated_by=excluded.updated_by`, orgID, c.Revision, c.UpdatedAt, actor)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM assignee_members WHERE org_id = ?`, orgID); err != nil {
			return err
		}
		for _, m := range c.Members {
			if _, err = tx.ExecContext(ctx, `INSERT INTO assignee_members (org_id, uid, login, name) VALUES (?, ?, ?, ?)`, orgID, m.UID, m.Login, m.Name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return models.AssigneeCatalog{}, err
	}
	return c, nil
}
