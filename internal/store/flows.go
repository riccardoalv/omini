package store

import (
	"context"
	"sort"
	"time"
)

// FlowsKeep is how long conversations are kept.
const FlowsKeep = 24 * time.Hour

// FlowRow is the traffic of one conversation (two IPs, a protocol and the
// service port) during a minute.
type FlowRow struct {
	A, B    string // A < B
	Proto   uint8
	Port    uint16
	BytesAB uint64
	BytesBA uint64
	Packets uint64
}

// SaveFlows stores a minute of conversations and drops those older than FlowsKeep.
func (s *Store) SaveFlows(ctx context.Context, minute time.Time, rows []FlowRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO flow_minutes (at, a, b, proto, port, bytes_ab, bytes_ba, packets) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (at, a, b, proto, port) DO UPDATE SET
			bytes_ab = bytes_ab + excluded.bytes_ab, bytes_ba = bytes_ba + excluded.bytes_ba, packets = packets + excluded.packets`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	at := minute.Truncate(time.Minute).Unix()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, at, r.A, r.B, r.Proto, r.Port, clamp(r.BytesAB), clamp(r.BytesBA), clamp(r.Packets)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM flow_minutes WHERE at < ?`, minute.Add(-FlowsKeep).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// Conversation is the traffic between two IPs over a period, with its busiest ports.
type Conversation struct {
	A       string     `json:"a"`
	B       string     `json:"b"`
	BytesAB uint64     `json:"bytes_ab"`
	BytesBA uint64     `json:"bytes_ba"`
	Packets uint64     `json:"packets"`
	Ports   []FlowPort `json:"ports"`
}

// FlowPort is the traffic of one service in a conversation.
type FlowPort struct {
	Proto uint8  `json:"proto"`
	Port  uint16 `json:"port"`
	Bytes uint64 `json:"bytes"`
}

// Conversations sums the conversations since a time, busiest first; with ip,
// only those of that address. At most limit are returned, each with its top 5 ports.
func (s *Store) Conversations(ctx context.Context, since time.Time, ip string, limit int) ([]Conversation, error) {
	q := `SELECT a, b, proto, port, SUM(bytes_ab), SUM(bytes_ba), SUM(packets) FROM flow_minutes WHERE at >= ?`
	args := []any{since.Unix()}
	if ip != "" {
		q += ` AND (a = ? OR b = ?)`
		args = append(args, ip, ip)
	}
	q += ` GROUP BY a, b, proto, port`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byPair := map[[2]string]*Conversation{}
	for rows.Next() {
		var (
			a, b            string
			proto, port     int64
			ab, ba, packets int64
		)
		if err := rows.Scan(&a, &b, &proto, &port, &ab, &ba, &packets); err != nil {
			return nil, err
		}
		key := [2]string{a, b}
		c := byPair[key]
		if c == nil {
			c = &Conversation{A: a, B: b}
			byPair[key] = c
		}
		c.BytesAB += uint64(ab)                                                                              //nolint:gosec // stored from uint64
		c.BytesBA += uint64(ba)                                                                              //nolint:gosec // stored from uint64
		c.Packets += uint64(packets)                                                                         //nolint:gosec // stored from uint64
		c.Ports = append(c.Ports, FlowPort{Proto: uint8(proto), Port: uint16(port), Bytes: uint64(ab + ba)}) //nolint:gosec // stored from these types
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Conversation, 0, len(byPair))
	for _, c := range byPair {
		sort.Slice(c.Ports, func(i, j int) bool { return c.Ports[i].Bytes > c.Ports[j].Bytes })
		if len(c.Ports) > 5 {
			c.Ports = c.Ports[:5]
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i].BytesAB+out[i].BytesBA, out[j].BytesAB+out[j].BytesBA
		if x != y {
			return x > y
		}
		return out[i].A+out[i].B < out[j].A+out[j].B
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
