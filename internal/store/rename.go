package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// nodeColumns are the tables and columns that hold a map node id.
var nodeColumns = []struct{ table, column string }{
	{"inventory", "parent_id"},
	{"layout", "node_id"},
	{"port_labels", "node_id"},
	{"mac_ports", "node_id"},
	{"traffic_minutes", "node_id"},
	{"traffic_hours", "node_id"},
	{"presence_events", "node_id"},
	{"alerts", "node_id"},
	{"attachments", "node_id"},
	{"config_changes", "node_id"},
	{"config_changes", "target_id"},
	{"config_backups", "node_id"},
	{"internet_minutes", "node"},
	{"internet_hours", "node"},
	{"internet_destinations", "node"},
}

// RenameNode moves everything kept for the node from to the node to: its
// inventory entry (with the user's alias, type and icon), position, port
// labels, traffic and presence history, alerts, changes, backups and areas.
// A device whose key changed (a plugin that picks its MAC differently) keeps
// its history this way. Rows the new node already has win over the old ones.
func (s *Store) RenameNode(ctx context.Context, from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	// When both are known (the new id was recorded before the old one left the
	// map), the new entry keeps the first sighting and whatever the user set
	// on the old one and not on the new.
	if _, err := tx.ExecContext(ctx, `
		UPDATE inventory SET
			first_seen  = MIN(inventory.first_seen, o.first_seen),
			alias       = COALESCE(inventory.alias, o.alias),
			device_type = COALESCE(inventory.device_type, o.device_type),
			icon        = COALESCE(inventory.icon, o.icon),
			note        = CASE WHEN inventory.note = '' THEN o.note ELSE inventory.note END,
			owner_id    = COALESCE(inventory.owner_id, o.owner_id)
		FROM (SELECT first_seen, alias, device_type, icon, note, owner_id FROM inventory WHERE id = ?) AS o
		WHERE inventory.id = ?`, from, to); err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE OR IGNORE inventory SET id = ? WHERE id = ?`, to, from); err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inventory WHERE id = ?`, from); err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	for _, c := range nodeColumns {
		// OR IGNORE skips a row whose primary key the new node already has;
		// the old row left behind is then dropped.
		q := fmt.Sprintf(`UPDATE OR IGNORE %s SET %s = ? WHERE %s = ?`, c.table, c.column, c.column)
		if _, err := tx.ExecContext(ctx, q, to, from); err != nil {
			return fmt.Errorf("%s: %w", c.table, err)
		}
		q = fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, c.table, c.column)
		if _, err := tx.ExecContext(ctx, q, from); err != nil {
			return fmt.Errorf("%s: %w", c.table, err)
		}
	}
	if err := renameInAreas(ctx, tx, from, to); err != nil {
		return err
	}
	return tx.Commit()
}

// renameInAreas replaces the node in the areas' member lists.
func renameInAreas(ctx context.Context, tx *sql.Tx, from, to string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, members FROM areas`)
	if err != nil {
		return fmt.Errorf("areas: %w", err)
	}
	type area struct {
		id      int64
		members []string
	}
	var changed []area
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return fmt.Errorf("areas: %w", err)
		}
		var members []string
		if json.Unmarshal([]byte(raw), &members) != nil {
			continue
		}
		hit, dup := false, false
		for _, m := range members {
			hit = hit || m == from
			dup = dup || m == to
		}
		if !hit {
			continue
		}
		out := make([]string, 0, len(members))
		for _, m := range members {
			switch {
			case m == from && dup:
				continue
			case m == from:
				out = append(out, to)
			default:
				out = append(out, m)
			}
		}
		changed = append(changed, area{id, out})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("areas: %w", err)
	}
	rows.Close()
	for _, a := range changed {
		raw, _ := json.Marshal(a.members)
		if _, err := tx.ExecContext(ctx, `UPDATE areas SET members = ? WHERE id = ?`, string(raw), a.id); err != nil {
			return fmt.Errorf("areas: %w", err)
		}
	}
	return nil
}
