package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AlertsKeep is how long resolved alerts are kept.
const AlertsKeep = 90 * 24 * time.Hour

// Alert is an insight that was (or is) true: opened when a rule first finds
// it, resolved when the rule no longer does.
type Alert struct {
	ID         int64          `json:"id"`
	Key        string         `json:"key"`
	Rule       string         `json:"rule"`
	Severity   string         `json:"severity"`
	NodeID     string         `json:"node_id,omitempty"`
	Params     map[string]any `json:"params"`
	OpenedAt   time.Time      `json:"opened_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
	Dismissed  bool           `json:"dismissed"`
}

// AlertChange tells what SyncAlerts did, for notifications.
type AlertChange struct {
	Alert  Alert
	Opened bool // false: resolved
}

// SyncAlerts makes the open alerts match the current insights: new keys are
// opened, known ones refreshed (severity and parameters may change) and open
// alerts whose insight is gone are resolved. It returns what was opened or
// resolved.
func (s *Store) SyncAlerts(ctx context.Context, current []Alert, now time.Time) ([]AlertChange, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	open, err := queryAlerts(ctx, tx, `WHERE resolved_at IS NULL`)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]Alert, len(open))
	for _, a := range open {
		byKey[a.Key] = a
	}
	var changes []AlertChange
	seen := map[string]bool{}
	ts := now.Unix()
	for _, a := range current {
		if seen[a.Key] {
			continue
		}
		seen[a.Key] = true
		params, err := json.Marshal(a.Params)
		if err != nil {
			return nil, err
		}
		if old, ok := byKey[a.Key]; ok {
			if _, err := tx.ExecContext(ctx, `UPDATE alerts SET severity = ?, params = ?, updated_at = ? WHERE id = ?`,
				a.Severity, string(params), ts, old.ID); err != nil {
				return nil, err
			}
			continue
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO alerts (key, rule, severity, node_id, params, opened_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			a.Key, a.Rule, a.Severity, nullable(a.NodeID), string(params), ts, ts)
		if err != nil {
			return nil, err
		}
		a.ID, _ = res.LastInsertId()
		a.OpenedAt, a.UpdatedAt = fromUnix(ts), fromUnix(ts)
		changes = append(changes, AlertChange{Alert: a, Opened: true})
	}
	for _, a := range open {
		if seen[a.Key] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE alerts SET resolved_at = ? WHERE id = ?`, ts, a.ID); err != nil {
			return nil, err
		}
		at := fromUnix(ts)
		a.ResolvedAt = &at
		changes = append(changes, AlertChange{Alert: a})
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM alerts WHERE resolved_at < ?`, now.Add(-AlertsKeep).Unix()); err != nil {
		return nil, err
	}
	return changes, tx.Commit()
}

// ListAlerts returns the open alerts, plus those resolved since the given
// time when it is not zero; newest first.
func (s *Store) ListAlerts(ctx context.Context, resolvedSince time.Time) ([]Alert, error) {
	if resolvedSince.IsZero() {
		return queryAlerts(ctx, s.db, `WHERE resolved_at IS NULL ORDER BY opened_at DESC, id DESC`)
	}
	return queryAlerts(ctx, s.db, `WHERE resolved_at IS NULL OR resolved_at >= ? ORDER BY opened_at DESC, id DESC`,
		resolvedSince.Unix())
}

// DismissAlert hides an open alert until it is resolved and found again.
func (s *Store) DismissAlert(ctx context.Context, id int64, dismissed bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alerts SET dismissed = ? WHERE id = ?`, boolInt(dismissed), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryAlerts(ctx context.Context, q querier, where string, args ...any) ([]Alert, error) {
	//nolint:gosec // where is a fixed string from this file; values are bound
	rows, err := q.QueryContext(ctx, `
		SELECT id, key, rule, severity, COALESCE(node_id, ''), params, opened_at, updated_at, resolved_at, dismissed
		FROM alerts `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var (
			a               Alert
			params          string
			opened, updated int64
			resolved        sql.NullInt64
			dismissed       int
		)
		if err := rows.Scan(&a.ID, &a.Key, &a.Rule, &a.Severity, &a.NodeID, &params, &opened, &updated,
			&resolved, &dismissed); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(params), &a.Params); err != nil {
			return nil, errors.Join(errors.New("alert params"), err)
		}
		a.OpenedAt, a.UpdatedAt, a.Dismissed = fromUnix(opened), fromUnix(updated), dismissed == 1
		if resolved.Valid {
			t := fromUnix(resolved.Int64)
			a.ResolvedAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
