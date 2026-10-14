package store

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Internet traffic per device, worked out from flows: per minute for a day
// (what is happening now), per hour for a month (who used the internet), and
// where each device went, by name, for a day.
const (
	InternetMinutesKeep = 24 * time.Hour
	InternetHoursKeep   = 35 * 24 * time.Hour
	DestinationsKeep    = 24 * time.Hour
)

// Usage is a device's internet traffic.
type Usage struct {
	Node string `json:"node"`
	RX   uint64 `json:"rx"` // from the internet
	TX   uint64 `json:"tx"` // to the internet
}

// Destination is the traffic of a device with one place on the internet (a
// domain name, or the address when it has none) and one service.
type Destination struct {
	Node  string `json:"-"`
	Dest  string `json:"dest"`
	Proto uint8  `json:"proto"`
	Port  uint16 `json:"port"`
	RX    uint64 `json:"rx"`
	TX    uint64 `json:"tx"`
}

// SaveInternet adds a minute of internet traffic, and drops what is older
// than it is kept.
func (s *Store) SaveInternet(ctx context.Context, minute time.Time, usage []Usage, dests []Destination) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	at, hour := minute.Truncate(time.Minute).Unix(), minute.Truncate(time.Hour).Unix()
	for _, table := range []struct {
		name string
		at   int64
	}{{"internet_minutes", at}, {"internet_hours", hour}} {
		for _, u := range usage {
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+table.name+` (at, node, rx, tx) VALUES (?, ?, ?, ?)
				ON CONFLICT (at, node) DO UPDATE SET rx = rx + excluded.rx, tx = tx + excluded.tx`,
				table.at, u.Node, clamp(u.RX), clamp(u.TX)); err != nil {
				return err
			}
		}
	}
	for _, d := range dests {
		if _, err := tx.ExecContext(ctx, `INSERT INTO internet_destinations (at, node, dest, port, proto, rx, tx) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (at, node, dest, port, proto) DO UPDATE SET rx = rx + excluded.rx, tx = tx + excluded.tx`,
			hour, d.Node, d.Dest, d.Port, d.Proto, clamp(d.RX), clamp(d.TX)); err != nil {
			return err
		}
	}
	for _, p := range []struct {
		q    string
		keep time.Duration
	}{
		{`DELETE FROM internet_minutes WHERE at < ?`, InternetMinutesKeep},
		{`DELETE FROM internet_hours WHERE at < ?`, InternetHoursKeep},
		{`DELETE FROM internet_destinations WHERE at < ?`, DestinationsKeep},
	} {
		if _, err := tx.ExecContext(ctx, p.q, minute.Add(-p.keep).Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// InternetUsage sums each device's internet traffic since a time, busiest
// first: from the minutes when since is within a day, else from the hours.
func (s *Store) InternetUsage(ctx context.Context, since, now time.Time) ([]Usage, error) {
	table := "internet_hours"
	from := since.Truncate(time.Hour).Unix()
	if now.Sub(since) <= InternetMinutesKeep {
		table, from = "internet_minutes", since.Unix()
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node, SUM(rx), SUM(tx) FROM `+table+` WHERE at >= ? GROUP BY node`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Usage
	for rows.Next() {
		var (
			u      Usage
			rx, tx int64
		)
		if err := rows.Scan(&u.Node, &rx, &tx); err != nil {
			return nil, err
		}
		u.RX, u.TX = uint64(rx), uint64(tx) //nolint:gosec // stored from uint64
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i].RX+out[i].TX, out[j].RX+out[j].TX
		if x != y {
			return x > y
		}
		return out[i].Node < out[j].Node
	})
	return out, rows.Err()
}

// Destinations sums where devices went since a time, busiest first: one
// device's (node), or every device's on the given ports.
func (s *Store) Destinations(ctx context.Context, since time.Time, node string, ports ...uint16) ([]Destination, error) {
	q := `SELECT node, dest, proto, port, SUM(rx), SUM(tx) FROM internet_destinations WHERE at >= ?`
	args := []any{since.Truncate(time.Hour).Unix()}
	if node != "" {
		q += ` AND node = ?`
		args = append(args, node)
	}
	if len(ports) > 0 {
		q += ` AND port IN (?` + strings.Repeat(`, ?`, len(ports)-1) + `)`
		for _, p := range ports {
			args = append(args, p)
		}
	}
	rows, err := s.db.QueryContext(ctx, q+` GROUP BY node, dest, proto, port`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Destination
	for rows.Next() {
		var (
			d           Destination
			proto, port int64
			rx, tx      int64
		)
		if err := rows.Scan(&d.Node, &d.Dest, &proto, &port, &rx, &tx); err != nil {
			return nil, err
		}
		d.Proto, d.Port = uint8(proto), uint16(port) //nolint:gosec // stored from these types
		d.RX, d.TX = uint64(rx), uint64(tx)          //nolint:gosec // stored from uint64
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i].RX+out[i].TX, out[j].RX+out[j].TX
		if x != y {
			return x > y
		}
		return out[i].Dest < out[j].Dest
	})
	return out, rows.Err()
}

// HourlyUpload is each device's upload per hour since a time (oldest first).
func (s *Store) HourlyUpload(ctx context.Context, since time.Time) (map[string]map[int64]uint64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node, at, tx FROM internet_hours WHERE at >= ?`, since.Truncate(time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[int64]uint64{}
	for rows.Next() {
		var (
			node   string
			at, tx int64
		)
		if err := rows.Scan(&node, &at, &tx); err != nil {
			return nil, err
		}
		if out[node] == nil {
			out[node] = map[int64]uint64{}
		}
		out[node][at] = uint64(tx) //nolint:gosec // stored from uint64
	}
	return out, rows.Err()
}
