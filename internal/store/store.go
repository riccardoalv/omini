// Package store persists Omini's state in a single SQLite file.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure Go SQLite driver (no CGO, cross-compiles to ARM)
)

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// migrations are applied in order; PRAGMA user_version records the last one applied.
// Never edit a released migration: append a new one.
var migrations = []string{
	`
	CREATE TABLE integrations (
		id         INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		type       TEXT    NOT NULL,
		config     TEXT    NOT NULL, -- JSON; secret fields sealed by internal/secret
		enabled    INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE TABLE snapshots (
		integration_id INTEGER PRIMARY KEY REFERENCES integrations(id) ON DELETE CASCADE,
		collected_at   INTEGER NOT NULL,
		ok             INTEGER NOT NULL,
		error          TEXT,
		duration_ms    INTEGER NOT NULL,
		devices        TEXT    NOT NULL -- JSON array of model.Device
	);

	CREATE TABLE inventory (
		id         TEXT PRIMARY KEY, -- topology node id
		kind       TEXT    NOT NULL, -- device | unmanaged | segment | client
		label      TEXT    NOT NULL DEFAULT '',
		mac        TEXT,
		ip         TEXT,
		hostname   TEXT,
		vendor     TEXT,
		parent_id  TEXT,             -- last known attachment
		port       TEXT,
		alias      TEXT,             -- name given by the user
		pinned     INTEGER NOT NULL DEFAULT 0,
		first_seen INTEGER NOT NULL,
		last_seen  INTEGER NOT NULL
	);
	CREATE INDEX inventory_last_seen ON inventory(last_seen);

	CREATE TABLE layout (
		node_id TEXT PRIMARY KEY,
		x       REAL NOT NULL,
		y       REAL NOT NULL
	);

	CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE users (
		id            INTEGER PRIMARY KEY,
		username      TEXT    NOT NULL UNIQUE,
		password_hash TEXT    NOT NULL,
		created_at    INTEGER NOT NULL
	);

	CREATE TABLE sessions (
		token_hash TEXT    PRIMARY KEY,
		user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at INTEGER NOT NULL
	);
	`,
	// 2: user corrections of the automatic classification.
	`
	ALTER TABLE inventory ADD COLUMN device_type TEXT; -- e.g. "phone"; overrides the detected type
	ALTER TABLE inventory ADD COLUMN icon TEXT;        -- logo slug, e.g. "android"; overrides the detected one
	`,
	// 3: named areas drawn on the map, kept per orientation like node positions.
	`
	CREATE TABLE areas (
		id         INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		color      TEXT    NOT NULL,
		direction  TEXT    NOT NULL, -- RIGHT | DOWN
		x          REAL    NOT NULL,
		y          REAL    NOT NULL,
		width      REAL    NOT NULL,
		height     REAL    NOT NULL,
		created_at INTEGER NOT NULL
	);
	`,
	// 4: per-user preferences.
	`
	ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT ''; -- UI language, e.g. "pt-BR"; '' = browser default
	`,
	// 5: areas remember their nodes, so they follow them when the map is laid out again.
	`
	ALTER TABLE areas ADD COLUMN members TEXT NOT NULL DEFAULT '[]'; -- JSON array of node ids
	`,
	// 6: devices the user hid from the map.
	`
	ALTER TABLE inventory ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0;
	`,
	// 7: the user's descriptions of device ports ("Uplink to rack", "TV room").
	`
	CREATE TABLE port_labels (
		node_id TEXT NOT NULL,
		port    TEXT NOT NULL,
		label   TEXT NOT NULL,
		PRIMARY KEY (node_id, port)
	);
	`,
	// 8: collection interval per integration (0 = the global default).
	`
	ALTER TABLE integrations ADD COLUMN interval_s INTEGER NOT NULL DEFAULT 0;
	`,
	// 9: where each MAC was last learned by a switch (kept across restarts).
	`
	CREATE TABLE mac_ports (
		mac     TEXT PRIMARY KEY,
		node_id TEXT NOT NULL,
		port    TEXT NOT NULL,
		seen_at INTEGER NOT NULL -- unix seconds
	);
	`,
	// 10: traffic history, presence timeline and alerts.
	`
	CREATE TABLE traffic_minutes ( -- one average per minute, kept 24 h
		node_id TEXT    NOT NULL,
		iface   TEXT    NOT NULL, -- '' = the node's own traffic (a Wi-Fi client as its AP measures it)
		at      INTEGER NOT NULL, -- unix seconds, start of the minute
		rx_bps  INTEGER NOT NULL,
		tx_bps  INTEGER NOT NULL,
		PRIMARY KEY (node_id, iface, at)
	) WITHOUT ROWID;
	CREATE INDEX traffic_minutes_at ON traffic_minutes(at);

	CREATE TABLE traffic_hours ( -- average and peak per hour, kept a year
		node_id    TEXT    NOT NULL,
		iface      TEXT    NOT NULL,
		at         INTEGER NOT NULL, -- start of the hour
		rx_bps     INTEGER NOT NULL,
		tx_bps     INTEGER NOT NULL,
		rx_max_bps INTEGER NOT NULL,
		tx_max_bps INTEGER NOT NULL,
		samples    INTEGER NOT NULL, -- minutes averaged so far
		PRIMARY KEY (node_id, iface, at)
	) WITHOUT ROWID;
	CREATE INDEX traffic_hours_at ON traffic_hours(at);

	CREATE TABLE presence_events (
		id      INTEGER PRIMARY KEY,
		node_id TEXT    NOT NULL,
		kind    TEXT    NOT NULL, -- join | leave
		at      INTEGER NOT NULL,
		first   INTEGER NOT NULL DEFAULT 0 -- the first time the device was ever seen
	);
	CREATE INDEX presence_events_node ON presence_events(node_id, at);
	CREATE INDEX presence_events_at ON presence_events(at);

	CREATE TABLE alerts (
		id          INTEGER PRIMARY KEY,
		key         TEXT    NOT NULL, -- rule + subject: one open alert per key
		rule        TEXT    NOT NULL,
		severity    TEXT    NOT NULL, -- critical | warning | info
		node_id     TEXT,
		params      TEXT    NOT NULL DEFAULT '{}',
		opened_at   INTEGER NOT NULL,
		updated_at  INTEGER NOT NULL,
		resolved_at INTEGER,
		dismissed   INTEGER NOT NULL DEFAULT 0
	);
	CREATE UNIQUE INDEX alerts_open ON alerts(key) WHERE resolved_at IS NULL;
	CREATE INDEX alerts_resolved ON alerts(resolved_at);
	`,
	// 11: notification channels (webhook, Telegram, e-mail).
	`
	CREATE TABLE notifiers (
		id              INTEGER PRIMARY KEY,
		type            TEXT    NOT NULL, -- webhook | telegram | email
		config          TEXT    NOT NULL, -- JSON; secret fields sealed by internal/secret
		min_severity    TEXT    NOT NULL DEFAULT 'warning',
		notify_resolved INTEGER NOT NULL DEFAULT 1,
		enabled         INTEGER NOT NULL DEFAULT 1,
		last_sent_at    INTEGER,
		last_error      TEXT,
		created_at      INTEGER NOT NULL
	);
	`,
	// 12: conversations from flow exports (NetFlow, IPFIX, sFlow), per minute, kept a day.
	`
	CREATE TABLE flow_minutes (
		at       INTEGER NOT NULL, -- start of the minute
		a        TEXT    NOT NULL, -- the lower IP of the pair
		b        TEXT    NOT NULL,
		proto    INTEGER NOT NULL,
		port     INTEGER NOT NULL, -- the service port (the lower of the two)
		bytes_ab INTEGER NOT NULL, -- a → b
		bytes_ba INTEGER NOT NULL,
		packets  INTEGER NOT NULL,
		PRIMARY KEY (at, a, b, proto, port)
	) WITHOUT ROWID;
	CREATE INDEX flow_minutes_a ON flow_minutes(a, at);
	CREATE INDEX flow_minutes_b ON flow_minutes(b, at);
	`,
	// 13: areas created automatically for each VLAN and subnet; removing one only dismisses it.
	`
	ALTER TABLE areas ADD COLUMN auto TEXT; -- "vlan:20" | "subnet:192.168.20.0/24"
	ALTER TABLE areas ADD COLUMN dismissed INTEGER NOT NULL DEFAULT 0;
	CREATE UNIQUE INDEX areas_auto ON areas(auto) WHERE auto IS NOT NULL;
	`,
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows a single writer; one connection avoids "database is locked".
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func unix(t time.Time) int64 { return t.Unix() }

func fromUnix(n int64) time.Time { return time.Unix(n, 0).UTC() }

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
