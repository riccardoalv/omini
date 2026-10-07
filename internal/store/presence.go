package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// PresenceKeep is how long the presence timeline is kept.
const PresenceKeep = 365 * 24 * time.Hour

// PresenceEvent is a device joining or leaving the network.
type PresenceEvent struct {
	ID     int64     `json:"id"`
	NodeID string    `json:"node_id"`
	Kind   string    `json:"kind"` // join | leave
	At     time.Time `json:"at"`
	First  bool      `json:"first,omitempty"` // the first time the device was ever seen
	// Filled from the inventory when listed.
	Label string `json:"label,omitempty"`
	MAC   string `json:"mac,omitempty"`
	IP    string `json:"ip,omitempty"`
}

// AddPresence records events and drops those older than PresenceKeep.
func (s *Store) AddPresence(ctx context.Context, events []PresenceEvent, now time.Time) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	for _, e := range events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO presence_events (node_id, kind, at, first) VALUES (?, ?, ?, ?)`,
			e.NodeID, e.Kind, e.At.Unix(), boolInt(e.First)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM presence_events WHERE at < ?`, now.Add(-PresenceKeep).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// PresenceState returns the last event of every node: whether it is
// considered present now.
func (s *Store) PresenceState(ctx context.Context) (map[string]PresenceEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.node_id, e.kind, e.at, e.first FROM presence_events e
		JOIN (SELECT node_id, MAX(id) AS id FROM presence_events GROUP BY node_id) last ON last.id = e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PresenceEvent{}
	for rows.Next() {
		e, err := scanPresence(rows)
		if err != nil {
			return nil, err
		}
		out[e.NodeID] = e
	}
	return out, rows.Err()
}

// PresenceQuery filters the timeline: one node, events before an id (paging),
// only first sightings.
type PresenceQuery struct {
	NodeID   string
	BeforeID int64
	Limit    int
	First    bool
}

// ListPresence returns events, newest first, with the device's name.
func (s *Store) ListPresence(ctx context.Context, q PresenceQuery) ([]PresenceEvent, error) {
	where := []string{"1 = 1"}
	args := []any{}
	if q.NodeID != "" {
		where = append(where, "e.node_id = ?")
		args = append(args, q.NodeID)
	}
	if q.BeforeID > 0 {
		where = append(where, "e.id < ?")
		args = append(args, q.BeforeID)
	}
	if q.First {
		where = append(where, "e.first = 1")
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	args = append(args, q.Limit)
	//nolint:gosec // the WHERE clauses are fixed strings; values are bound
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.node_id, e.kind, e.at, e.first,
		       COALESCE(NULLIF(i.alias, ''), i.label, ''), COALESCE(i.mac, ''), COALESCE(i.ip, '')
		FROM presence_events e LEFT JOIN inventory i ON i.id = e.node_id
		WHERE `+strings.Join(where, " AND ")+` ORDER BY e.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PresenceEvent{}
	for rows.Next() {
		var (
			e     PresenceEvent
			at    int64
			first int
		)
		if err := rows.Scan(&e.ID, &e.NodeID, &e.Kind, &at, &first, &e.Label, &e.MAC, &e.IP); err != nil {
			return nil, err
		}
		e.At, e.First = fromUnix(at), first == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanPresence(rows *sql.Rows) (PresenceEvent, error) {
	var (
		e     PresenceEvent
		at    int64
		first int
	)
	err := rows.Scan(&e.ID, &e.NodeID, &e.Kind, &at, &first)
	e.At, e.First = fromUnix(at), first == 1
	return e, err
}
