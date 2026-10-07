package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("not found")

// Integration is a configured integration instance. Config values of secret
// fields are stored sealed; callers seal/open them with internal/secret.
type Integration struct {
	ID        int64              `json:"id"`
	Name      string             `json:"name"`
	Type      string             `json:"type"`
	Config    integration.Config `json:"config"`
	Enabled   bool               `json:"enabled"`
	IntervalS int                `json:"interval_s"` // seconds between collections; 0 = default
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

func (s *Store) ListIntegrations(ctx context.Context) ([]Integration, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, type, config, enabled, created_at, updated_at, interval_s FROM integrations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Integration
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetIntegration(ctx context.Context, id int64) (Integration, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, type, config, enabled, created_at, updated_at, interval_s FROM integrations WHERE id = ?`, id)
	i, err := scanIntegration(row)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	return i, err
}

func (s *Store) CreateIntegration(ctx context.Context, i Integration) (Integration, error) {
	cfg, err := json.Marshal(i.Config)
	if err != nil {
		return i, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO integrations (name, type, config, enabled, created_at, updated_at, interval_s) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		i.Name, i.Type, string(cfg), boolInt(i.Enabled), unix(now), unix(now), i.IntervalS)
	if err != nil {
		return i, err
	}
	i.ID, err = res.LastInsertId()
	i.CreatedAt, i.UpdatedAt = now, now
	return i, err
}

func (s *Store) UpdateIntegration(ctx context.Context, i Integration) (Integration, error) {
	cfg, err := json.Marshal(i.Config)
	if err != nil {
		return i, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	res, err := s.db.ExecContext(ctx,
		`UPDATE integrations SET name = ?, config = ?, enabled = ?, updated_at = ?, interval_s = ? WHERE id = ?`,
		i.Name, string(cfg), boolInt(i.Enabled), unix(now), i.IntervalS, i.ID)
	if err != nil {
		return i, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return i, ErrNotFound
	}
	return s.GetIntegration(ctx, i.ID)
}

func (s *Store) DeleteIntegration(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM integrations WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface{ Scan(dest ...any) error }

func scanIntegration(r scanner) (Integration, error) {
	var (
		i                  Integration
		cfg                string
		enabled            int
		created, updatedAt int64
	)
	if err := r.Scan(&i.ID, &i.Name, &i.Type, &cfg, &enabled, &created, &updatedAt, &i.IntervalS); err != nil {
		return i, err
	}
	if err := json.Unmarshal([]byte(cfg), &i.Config); err != nil {
		return i, err
	}
	i.Enabled = enabled == 1
	i.CreatedAt, i.UpdatedAt = fromUnix(created), fromUnix(updatedAt)
	return i, nil
}

// Snapshot is the last collection result of one integration.
type Snapshot struct {
	IntegrationID int64          `json:"integration_id"`
	CollectedAt   time.Time      `json:"collected_at"`
	OK            bool           `json:"ok"`
	Error         string         `json:"error,omitempty"`
	DurationMs    int64          `json:"duration_ms"`
	Devices       []model.Device `json:"devices"`
}

func (s *Store) SaveSnapshot(ctx context.Context, sn Snapshot) error {
	devices, err := json.Marshal(sn.Devices)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO snapshots (integration_id, collected_at, ok, error, duration_ms, devices)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (integration_id) DO UPDATE SET
			collected_at = excluded.collected_at, ok = excluded.ok, error = excluded.error,
			duration_ms = excluded.duration_ms, devices = excluded.devices`,
		sn.IntegrationID, unix(sn.CollectedAt), boolInt(sn.OK), sn.Error, sn.DurationMs, string(devices))
	return err
}

func (s *Store) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT integration_id, collected_at, ok, COALESCE(error, ''), duration_ms, devices FROM snapshots`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var (
			sn        Snapshot
			collected int64
			ok        int
			devices   string
		)
		if err := rows.Scan(&sn.IntegrationID, &collected, &ok, &sn.Error, &sn.DurationMs, &devices); err != nil {
			return nil, err
		}
		sn.CollectedAt, sn.OK = fromUnix(collected), ok == 1
		if err := json.Unmarshal([]byte(devices), &sn.Devices); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}
