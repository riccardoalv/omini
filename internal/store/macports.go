package store

import (
	"context"
	"time"
)

// MACPort is where a MAC was last learned by a switch: a port of a device on
// the map, and when.
type MACPort struct {
	MAC    string
	NodeID string
	Port   string
	SeenAt time.Time
}

// MACPorts returns the MACs learned since the given time.
func (s *Store) MACPorts(ctx context.Context, since time.Time) ([]MACPort, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mac, node_id, port, seen_at FROM mac_ports WHERE seen_at >= ? ORDER BY mac`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MACPort
	for rows.Next() {
		var p MACPort
		var at int64
		if err := rows.Scan(&p.MAC, &p.NodeID, &p.Port, &at); err != nil {
			return nil, err
		}
		p.SeenAt = time.Unix(at, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveMACPorts records where MACs were learned now and forgets those last
// learned before the given time.
func (s *Store) SaveMACPorts(ctx context.Context, ports []MACPort, forgetBefore time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO mac_ports (mac, node_id, port, seen_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (mac) DO UPDATE SET node_id = excluded.node_id, port = excluded.port, seen_at = excluded.seen_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range ports {
		if _, err := stmt.ExecContext(ctx, p.MAC, p.NodeID, p.Port, p.SeenAt.Unix()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mac_ports WHERE seen_at < ?`, forgetBefore.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// Attachment is where a client was last seen for sure: the access point it
// was associated with (WiFi, with the network and band), or the switch port
// it was plugged into.
type Attachment struct {
	MAC    string
	NodeID string
	Port   string
	WiFi   bool
	SSID   string
	Band   string
	SeenAt time.Time
}

// Attachments returns where clients were last seen for sure.
func (s *Store) Attachments(ctx context.Context) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mac, node_id, port, wifi, ssid, band, seen_at FROM attachments ORDER BY mac`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var (
			a        Attachment
			wifi, at int64
		)
		if err := rows.Scan(&a.MAC, &a.NodeID, &a.Port, &wifi, &a.SSID, &a.Band, &at); err != nil {
			return nil, err
		}
		a.WiFi, a.SeenAt = wifi == 1, fromUnix(at)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveAttachments records where clients were seen for sure now, and forgets
// those not confirmed since the given time.
func (s *Store) SaveAttachments(ctx context.Context, list []Attachment, forgetBefore time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO attachments (mac, node_id, port, wifi, ssid, band, seen_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (mac) DO UPDATE SET node_id = excluded.node_id, port = excluded.port, wifi = excluded.wifi,
			ssid = excluded.ssid, band = excluded.band, seen_at = excluded.seen_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, a := range list {
		wifi := 0
		if a.WiFi {
			wifi = 1
		}
		if _, err := stmt.ExecContext(ctx, a.MAC, a.NodeID, a.Port, wifi, a.SSID, a.Band, a.SeenAt.Unix()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE seen_at < ?`, forgetBefore.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
