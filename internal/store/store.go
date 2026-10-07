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
