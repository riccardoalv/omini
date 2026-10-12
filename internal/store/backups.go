package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// BackupsKept is how many versions of a device's configuration are kept.
const BackupsKept = 50

// Backup is one version of a device's configuration. Content is sealed by
// the caller (it holds secrets) and empty in lists.
type Backup struct {
	ID            int64     `json:"id"`
	NodeID        string    `json:"node_id"`
	IntegrationID int64     `json:"integration_id"`
	TakenAt       time.Time `json:"taken_at"`
	Format        string    `json:"format"`
	SHA256        string    `json:"-"`
	Size          int       `json:"size"`
	// ByOmini: Omini changed the device since the version before.
	ByOmini bool   `json:"by_omini"`
	Content string `json:"-"`
}

// AddBackup keeps a version of a device's configuration when it differs from
// the last one (by SHA256), and drops the oldest past BackupsKept. It
// reports whether the version was new.
func (s *Store) AddBackup(ctx context.Context, b Backup) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var last string
	err = tx.QueryRowContext(ctx, `SELECT sha256 FROM config_backups WHERE node_id = ? ORDER BY taken_at DESC, id DESC LIMIT 1`, b.NodeID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if last == b.SHA256 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_backups (node_id, integration_id, taken_at, format, sha256, size, by_omini, content)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		b.NodeID, b.IntegrationID, b.TakenAt.Unix(), b.Format, b.SHA256, b.Size, boolInt(b.ByOmini), b.Content); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM config_backups WHERE node_id = ? AND id NOT IN (
			SELECT id FROM config_backups WHERE node_id = ? ORDER BY taken_at DESC, id DESC LIMIT ?)`,
		b.NodeID, b.NodeID, BackupsKept); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func scanBackups(rows *sql.Rows, withContent bool) ([]Backup, error) {
	defer func() { _ = rows.Close() }()
	var out []Backup
	for rows.Next() {
		var b Backup
		var at int64
		var by int
		dest := []any{&b.ID, &b.NodeID, &b.IntegrationID, &at, &b.Format, &b.SHA256, &b.Size, &by}
		if withContent {
			dest = append(dest, &b.Content)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		b.TakenAt, b.ByOmini = time.Unix(at, 0).UTC(), by != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

const backupColumns = `id, node_id, integration_id, taken_at, format, sha256, size, by_omini`

// Backups lists a device's versions, newest first (without their content).
func (s *Store) Backups(ctx context.Context, nodeID string) ([]Backup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+backupColumns+` FROM config_backups WHERE node_id = ? ORDER BY taken_at DESC, id DESC`, nodeID)
	if err != nil {
		return nil, err
	}
	return scanBackups(rows, false)
}

// GetBackup is one version with its (sealed) content.
func (s *Store) GetBackup(ctx context.Context, id int64) (Backup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+backupColumns+`, content FROM config_backups WHERE id = ?`, id)
	if err != nil {
		return Backup{}, err
	}
	list, err := scanBackups(rows, true)
	if err != nil {
		return Backup{}, err
	}
	if len(list) == 0 {
		return Backup{}, ErrNotFound
	}
	return list[0], nil
}

// ChangedOutside are the devices whose configuration changed without Omini
// since a moment: a version not made by Omini that has one before it.
func (s *Store) ChangedOutside(ctx context.Context, since time.Time) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.node_id, MAX(b.taken_at) FROM config_backups b
		WHERE b.taken_at >= ? AND b.by_omini = 0
		  AND EXISTS (SELECT 1 FROM config_backups p WHERE p.node_id = b.node_id AND p.taken_at < b.taken_at)
		GROUP BY b.node_id`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at int64
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = time.Unix(at, 0).UTC()
	}
	return out, rows.Err()
}
