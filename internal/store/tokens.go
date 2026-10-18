package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"
)

// APIToken is a named, read-only API token. Only the hash of the token is
// stored; the token itself is shown once, when it is created.
type APIToken struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Hash       string     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// CreateAPIToken stores a token by its hash.
func (s *Store) CreateAPIToken(ctx context.Context, name, hash string, now time.Time) (APIToken, error) {
	now = now.UTC().Truncate(time.Second)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_tokens (name, hash, created_at) VALUES (?, ?, ?)`, name, hash, unix(now))
	if err != nil {
		return APIToken{}, err
	}
	id, err := res.LastInsertId()
	return APIToken{ID: id, Name: name, Hash: hash, CreatedAt: now}, err
}

// ListAPITokens returns the tokens, oldest first.
func (s *Store) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, hash, created_at, last_used_at FROM api_tokens ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// APITokenByHash returns the token with that hash (ErrNotFound: unknown or
// revoked). The stored hash is compared in constant time.
func (s *Store) APITokenByHash(ctx context.Context, hash string) (APIToken, error) {
	t, err := scanAPIToken(s.db.QueryRowContext(ctx,
		`SELECT id, name, hash, created_at, last_used_at FROM api_tokens WHERE hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	if err != nil {
		return APIToken{}, err
	}
	if subtle.ConstantTimeCompare([]byte(t.Hash), []byte(hash)) != 1 {
		return APIToken{}, ErrNotFound
	}
	return t, nil
}

// TouchAPIToken records the token's last use.
func (s *Store) TouchAPIToken(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, unix(at), id)
	return err
}

// DeleteAPIToken revokes a token.
func (s *Store) DeleteAPIToken(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanAPIToken(row interface{ Scan(...any) error }) (APIToken, error) {
	var (
		t        APIToken
		created  int64
		lastUsed sql.NullInt64
	)
	if err := row.Scan(&t.ID, &t.Name, &t.Hash, &created, &lastUsed); err != nil {
		return t, err
	}
	t.CreatedAt = fromUnix(created)
	if lastUsed.Valid {
		at := fromUnix(lastUsed.Int64)
		t.LastUsedAt = &at
	}
	return t, nil
}
