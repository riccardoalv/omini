package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// InventoryEntry is a device ever seen on the network. Entries are kept until
// the user deletes them.
type InventoryEntry struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	MAC      string `json:"mac,omitempty"`
	IP       string `json:"ip,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Vendor   string `json:"vendor,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	Port     string `json:"port,omitempty"`
	Alias    string `json:"alias,omitempty"`
	Pinned   bool   `json:"pinned"`
	// User corrections of the classification; empty means automatic.
	DeviceType string    `json:"device_type,omitempty"`
	Icon       string    `json:"icon,omitempty"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

// MarkSeen records that entries were present at time at. New entries are
// created; existing ones get their observed fields refreshed. User fields
// (alias, pinned) are never overwritten.
func (s *Store) MarkSeen(ctx context.Context, entries []InventoryEntry, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO inventory (id, kind, label, mac, ip, hostname, vendor, parent_id, port, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			kind = excluded.kind,
			label = excluded.label,
			mac = COALESCE(excluded.mac, inventory.mac),
			ip = COALESCE(excluded.ip, inventory.ip),
			hostname = COALESCE(excluded.hostname, inventory.hostname),
			vendor = COALESCE(excluded.vendor, inventory.vendor),
			parent_id = excluded.parent_id,
			port = excluded.port,
			last_seen = excluded.last_seen`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	ts := unix(at)
	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.ID, e.Kind, e.Label, nullable(e.MAC), nullable(e.IP),
			nullable(e.Hostname), nullable(e.Vendor), nullable(e.ParentID), nullable(e.Port), ts, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListInventory(ctx context.Context) ([]InventoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, label, COALESCE(mac, ''), COALESCE(ip, ''), COALESCE(hostname, ''), COALESCE(vendor, ''),
		       COALESCE(parent_id, ''), COALESCE(port, ''), COALESCE(alias, ''), pinned, first_seen, last_seen,
		       COALESCE(device_type, ''), COALESCE(icon, '')
		FROM inventory ORDER BY last_seen DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InventoryEntry
	for rows.Next() {
		var (
			e             InventoryEntry
			pinned        int
			first, latest int64
		)
		if err := rows.Scan(&e.ID, &e.Kind, &e.Label, &e.MAC, &e.IP, &e.Hostname, &e.Vendor,
			&e.ParentID, &e.Port, &e.Alias, &pinned, &first, &latest, &e.DeviceType, &e.Icon); err != nil {
			return nil, err
		}
		e.Pinned, e.FirstSeen, e.LastSeen = pinned == 1, fromUnix(first), fromUnix(latest)
		out = append(out, e)
	}
	return out, rows.Err()
}

// InventoryUpdate holds the user fields to change; nil means unchanged and an
// empty string resets the field to automatic.
type InventoryUpdate struct {
	Alias      *string `json:"alias"`
	Pinned     *bool   `json:"pinned"`
	DeviceType *string `json:"device_type"`
	Icon       *string `json:"icon"`
}

// UpdateInventory changes the user fields of an entry.
func (s *Store) UpdateInventory(ctx context.Context, id string, u InventoryUpdate) (InventoryEntry, error) {
	alias, pinned := u.Alias, u.Pinned
	for col, v := range map[string]*string{"device_type": u.DeviceType, "icon": u.Icon} {
		if v == nil {
			continue
		}
		//nolint:gosec // col comes from the fixed map above, not from the request
		if _, err := s.db.ExecContext(ctx, `UPDATE inventory SET `+col+` = ? WHERE id = ?`,
			nullable(strings.TrimSpace(*v)), id); err != nil {
			return InventoryEntry{}, err
		}
	}
	if alias != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE inventory SET alias = ? WHERE id = ?`,
			nullable(strings.TrimSpace(*alias)), id); err != nil {
			return InventoryEntry{}, err
		}
	}
	if pinned != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE inventory SET pinned = ? WHERE id = ?`, boolInt(*pinned), id); err != nil {
			return InventoryEntry{}, err
		}
	}
	all, err := s.ListInventory(ctx)
	if err != nil {
		return InventoryEntry{}, err
	}
	for _, e := range all {
		if e.ID == id {
			return e, nil
		}
	}
	return InventoryEntry{}, ErrNotFound
}

func (s *Store) DeleteInventory(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM inventory WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Point is a node position on the map.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (s *Store) GetLayout(ctx context.Context) (map[string]Point, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, x, y FROM layout`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Point{}
	for rows.Next() {
		var (
			id string
			p  Point
		)
		if err := rows.Scan(&id, &p.X, &p.Y); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}

// SaveLayout upserts the given positions; other saved positions are kept.
func (s *Store) SaveLayout(ctx context.Context, points map[string]Point) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	for id, p := range points {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO layout (node_id, x, y) VALUES (?, ?, ?)
			ON CONFLICT (node_id) DO UPDATE SET x = excluded.x, y = excluded.y`, id, p.X, p.Y); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ResetLayout forgets all saved positions, so the map is laid out automatically again.
func (s *Store) ResetLayout(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM layout`)
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
