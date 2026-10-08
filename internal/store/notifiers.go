package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
)

// Notifier is a channel alerts are sent to. Config values of secret fields
// are stored sealed, like integrations'.
type Notifier struct {
	ID             int64              `json:"id"`
	Type           string             `json:"type"`
	Config         integration.Config `json:"config"`
	MinSeverity    string             `json:"min_severity"` // critical | warning | info
	NotifyResolved bool               `json:"notify_resolved"`
	Enabled        bool               `json:"enabled"`
	LastSentAt     *time.Time         `json:"last_sent_at,omitempty"`
	LastError      string             `json:"last_error,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
}

func (s *Store) ListNotifiers(ctx context.Context) ([]Notifier, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, config, min_severity, notify_resolved, enabled, last_sent_at, COALESCE(last_error, ''), created_at
		FROM notifiers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notifier{}
	for rows.Next() {
		n, err := scanNotifier(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) GetNotifier(ctx context.Context, id int64) (Notifier, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, config, min_severity, notify_resolved, enabled, last_sent_at, COALESCE(last_error, ''), created_at
		FROM notifiers WHERE id = ?`, id)
	n, err := scanNotifier(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Notifier{}, ErrNotFound
	}
	return n, err
}

func (s *Store) CreateNotifier(ctx context.Context, n Notifier) (Notifier, error) {
	cfg, err := json.Marshal(n.Config)
	if err != nil {
		return Notifier{}, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO notifiers (type, config, min_severity, notify_resolved, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		n.Type, string(cfg), n.MinSeverity, boolInt(n.NotifyResolved), boolInt(n.Enabled), time.Now().Unix())
	if err != nil {
		return Notifier{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetNotifier(ctx, id)
}

func (s *Store) UpdateNotifier(ctx context.Context, n Notifier) (Notifier, error) {
	cfg, err := json.Marshal(n.Config)
	if err != nil {
		return Notifier{}, err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE notifiers SET config = ?, min_severity = ?, notify_resolved = ?, enabled = ? WHERE id = ?`,
		string(cfg), n.MinSeverity, boolInt(n.NotifyResolved), boolInt(n.Enabled), n.ID)
	if err != nil {
		return Notifier{}, err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return Notifier{}, ErrNotFound
	}
	return s.GetNotifier(ctx, n.ID)
}

func (s *Store) DeleteNotifier(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM notifiers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// NotifierSent records the outcome of a delivery (sendErr nil: delivered).
func (s *Store) NotifierSent(ctx context.Context, id int64, at time.Time, sendErr error) error {
	var msg any
	if sendErr != nil {
		msg = sendErr.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notifiers SET last_sent_at = ?, last_error = ? WHERE id = ?`, at.Unix(), msg, id)
	return err
}

// AdminLocale is the language of the first user (the admin), for messages
// written by the server (notifications); "" when unknown.
func (s *Store) AdminLocale(ctx context.Context) string {
	var locale string
	if err := s.db.QueryRowContext(ctx, `SELECT locale FROM users ORDER BY id LIMIT 1`).Scan(&locale); err != nil {
		return ""
	}
	return locale
}

func scanNotifier(r scanner) (Notifier, error) {
	var (
		n                 Notifier
		cfg               string
		resolved, enabled int
		sent              sql.NullInt64
		created           int64
	)
	if err := r.Scan(&n.ID, &n.Type, &cfg, &n.MinSeverity, &resolved, &enabled, &sent, &n.LastError, &created); err != nil {
		return Notifier{}, err
	}
	if err := json.Unmarshal([]byte(cfg), &n.Config); err != nil {
		return Notifier{}, err
	}
	n.NotifyResolved, n.Enabled, n.CreatedAt = resolved == 1, enabled == 1, fromUnix(created)
	if sent.Valid {
		t := fromUnix(sent.Int64)
		n.LastSentAt = &t
	}
	return n, nil
}
