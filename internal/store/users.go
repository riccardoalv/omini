package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Locale       string // UI language; empty means the browser default
	CreatedAt    time.Time
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (User, error) {
	now := time.Now().UTC().Truncate(time.Second)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`, username, passwordHash, unix(now))
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	return User{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: now}, err
}

func (s *Store) GetUserByName(ctx context.Context, username string) (User, error) {
	var (
		u       User
		created int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash, locale, created_at FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Locale, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	u.CreatedAt = fromUnix(created)
	return u, err
}

// SetUserLocale saves the user's UI language.
func (s *Store) SetUserLocale(ctx context.Context, userID int64, locale string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET locale = ? WHERE id = ?`, locale, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateSession stores a session by the hash of its token (never the token itself).
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, tokenHash, userID, unix(expires))
	return err
}

// GetSessionUser returns the user of a non-expired session.
func (s *Store) GetSessionUser(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	var (
		u       User
		created int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.password_hash, u.locale, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, unix(now)).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Locale, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	u.CreatedAt = fromUnix(created)
	return u, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, unix(now))
	return err
}
