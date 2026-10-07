package store

import (
	"context"
	"time"
)

// HistoryDay is how long per-minute traffic is kept; HistoryYear the hourly one.
const (
	HistoryDay  = 24 * time.Hour
	HistoryYear = 365 * 24 * time.Hour
)

// TrafficSample is the rate of one interface of a node at a moment. Iface ""
// is the node's own traffic (a Wi-Fi client, as its access point measures it).
type TrafficSample struct {
	NodeID string
	Iface  string
	RxBps  uint64
	TxBps  uint64
}

// TrafficPoint is one point of a traffic chart: the average over its minute
// (or hour), and for hours the peak minute.
type TrafficPoint struct {
	At       time.Time `json:"at"`
	RxBps    uint64    `json:"rx_bps"`
	TxBps    uint64    `json:"tx_bps"`
	RxMaxBps uint64    `json:"rx_max_bps,omitempty"`
	TxMaxBps uint64    `json:"tx_max_bps,omitempty"`
}

// RecordTraffic stores the rates seen at time at: one point per minute (a
// later sample in the same minute replaces it) and the hourly average and
// peak. Points older than a day (minutes) or a year (hours) are dropped.
func (s *Store) RecordTraffic(ctx context.Context, samples []TrafficSample, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	minute := at.Truncate(time.Minute).Unix()
	hour := at.Truncate(time.Hour).Unix()
	minStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO traffic_minutes (node_id, iface, at, rx_bps, tx_bps) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (node_id, iface, at) DO UPDATE SET rx_bps = excluded.rx_bps, tx_bps = excluded.tx_bps`)
	if err != nil {
		return err
	}
	defer minStmt.Close()
	// The hourly row is rebuilt from the minutes of that hour: replacing a
	// minute never counts it twice.
	hourStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO traffic_hours (node_id, iface, at, rx_bps, tx_bps, rx_max_bps, tx_max_bps, samples)
		SELECT node_id, iface, ?, AVG(rx_bps), AVG(tx_bps), MAX(rx_bps), MAX(tx_bps), COUNT(*)
		FROM traffic_minutes WHERE node_id = ? AND iface = ? AND at >= ? AND at < ?
		GROUP BY node_id, iface
		ON CONFLICT (node_id, iface, at) DO UPDATE SET
			rx_bps = excluded.rx_bps, tx_bps = excluded.tx_bps,
			rx_max_bps = excluded.rx_max_bps, tx_max_bps = excluded.tx_max_bps, samples = excluded.samples`)
	if err != nil {
		return err
	}
	defer hourStmt.Close()
	for _, p := range samples {
		if _, err := minStmt.ExecContext(ctx, p.NodeID, p.Iface, minute, clamp(p.RxBps), clamp(p.TxBps)); err != nil {
			return err
		}
		if _, err := hourStmt.ExecContext(ctx, hour, p.NodeID, p.Iface, hour, hour+3600); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_minutes WHERE at < ?`, at.Add(-HistoryDay).Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_hours WHERE at < ?`, at.Add(-HistoryYear).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// TrafficHistory returns the points of one interface since the given time:
// per minute when since is within the last day, else per hour.
func (s *Store) TrafficHistory(ctx context.Context, nodeID, iface string, since, now time.Time) ([]TrafficPoint, error) {
	hourly := now.Sub(since) > HistoryDay
	q := `SELECT at, rx_bps, tx_bps, rx_bps, tx_bps FROM traffic_minutes
		WHERE node_id = ? AND iface = ? AND at >= ? ORDER BY at`
	if hourly {
		q = `SELECT at, rx_bps, tx_bps, rx_max_bps, tx_max_bps FROM traffic_hours
			WHERE node_id = ? AND iface = ? AND at >= ? ORDER BY at`
	}
	rows, err := s.db.QueryContext(ctx, q, nodeID, iface, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrafficPoint{}
	for rows.Next() {
		var (
			at                 int64
			rx, tx, rxMx, txMx int64
		)
		if err := rows.Scan(&at, &rx, &tx, &rxMx, &txMx); err != nil {
			return nil, err
		}
		p := TrafficPoint{At: fromUnix(at), RxBps: uint64(rx), TxBps: uint64(tx)} //nolint:gosec // stored from uint64
		if hourly {
			p.RxMaxBps, p.TxMaxBps = uint64(rxMx), uint64(txMx) //nolint:gosec // stored from uint64
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// clamp keeps a rate within SQLite's signed 64-bit integers.
func clamp(v uint64) int64 {
	if v > 1<<62 {
		return 1 << 62
	}
	return int64(v)
}
