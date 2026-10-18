package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ConfigChangesKeep is how long the history of configuration changes is kept.
const ConfigChangesKeep = 365 * 24 * time.Hour

// ConfigChange is one configuration change made from Omini, applied or not.
type ConfigChange struct {
	ID     int64  `json:"id"`
	NodeID string `json:"node_id"`
	// TargetID is the device the change was for, when another node made it (a
	// reservation or a pause on the router, a name on the DNS server).
	TargetID      string          `json:"target_id,omitempty"`
	IntegrationID int64           `json:"integration_id"`
	Kind          string          `json:"kind"`
	Port          string          `json:"port,omitempty"`
	Before        json.RawMessage `json:"before,omitempty"` // nil when unknown
	After         json.RawMessage `json:"after"`
	Error         string          `json:"error,omitempty"` // empty when it was applied
	At            time.Time       `json:"at"`
}

// AddConfigChange records a change and forgets those older than ConfigChangesKeep.
func (s *Store) AddConfigChange(ctx context.Context, c ConfigChange) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	res, err := tx.ExecContext(ctx, `
		INSERT INTO config_changes (node_id, target_id, integration_id, kind, port, before, after, error, at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.NodeID, c.TargetID, c.IntegrationID, c.Kind, c.Port, string(c.Before), string(c.After), c.Error, c.At.Unix())
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM config_changes WHERE at < ?`, c.At.Add(-ConfigChangesKeep).Unix()); err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// ConfigChanges returns the changes made on a node or for it (through
// another one), newest first, at most limit.
func (s *Store) ConfigChanges(ctx context.Context, nodeID string, limit int) ([]ConfigChange, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_id, target_id, integration_id, kind, port, before, after, error, at
		FROM config_changes WHERE node_id = ? OR target_id = ? ORDER BY at DESC, id DESC LIMIT ?`, nodeID, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConfigChange{}
	for rows.Next() {
		var c ConfigChange
		var before, after string
		var at int64
		if err := rows.Scan(&c.ID, &c.NodeID, &c.TargetID, &c.IntegrationID, &c.Kind, &c.Port, &before, &after, &c.Error, &at); err != nil {
			return nil, err
		}
		if before != "" {
			c.Before = json.RawMessage(before)
		}
		c.After, c.At = json.RawMessage(after), time.Unix(at, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChangedByOmini reports whether Omini changed a node's configuration after
// since: a change applied on the node, or through the integration that backs
// it up (a UniFi or Omada site's settings live in one device's backup, but
// Omini changes them through whichever device of the site). Wake-on-LAN
// changes nothing.
func (s *Store) ChangedByOmini(ctx context.Context, nodeID string, integrationID int64, since time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM config_changes
		WHERE (node_id = ? OR integration_id = ?) AND error = '' AND kind != 'wol.send' AND at > ?`,
		nodeID, integrationID, since.Unix()).Scan(&n)
	return n > 0, err
}

// LastConfigChange is the latest change of a kind made on a node (for a target
// and port), if any.
func (s *Store) LastConfigChange(ctx context.Context, nodeID, targetID, kind, port string) (ConfigChange, bool, error) {
	var c ConfigChange
	var before, after string
	var at int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, node_id, target_id, integration_id, kind, port, before, after, error, at
		FROM config_changes WHERE node_id = ? AND target_id = ? AND kind = ? AND port = ?
		ORDER BY at DESC, id DESC LIMIT 1`, nodeID, targetID, kind, port).
		Scan(&c.ID, &c.NodeID, &c.TargetID, &c.IntegrationID, &c.Kind, &c.Port, &before, &after, &c.Error, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	if before != "" {
		c.Before = json.RawMessage(before)
	}
	c.After, c.At = json.RawMessage(after), time.Unix(at, 0)
	return c, true, nil
}
