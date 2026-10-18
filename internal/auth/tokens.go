package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/riccardoalv/omini/internal/store"
)

// APITokenPrefix starts every API token, so it is easy to recognize (and to
// find in a leaked file).
const APITokenPrefix = "omini_"

// APITokenNameMax is the longest token name, in characters.
const APITokenNameMax = 64

// touchEvery is how often a token's last use is written at most.
const touchEvery = time.Minute

var ErrTokenName = errors.New("a token needs a name of up to 64 characters")

// tokenUses remembers when each token's last use was written, so a busy
// client does not write to the database on every request.
type tokenUses struct {
	mu   sync.Mutex
	last map[int64]time.Time
}

// CreateAPIToken makes a new read-only token. The token is returned only
// here: the store keeps its hash.
func (s *Service) CreateAPIToken(ctx context.Context, name string) (store.APIToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > APITokenNameMax {
		return store.APIToken{}, "", ErrTokenName
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return store.APIToken{}, "", err
	}
	token := APITokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	t, err := s.store.CreateAPIToken(ctx, name, hashToken(token), s.now())
	if err != nil {
		return store.APIToken{}, "", err
	}
	return t, token, nil
}

// AuthenticateAPIToken returns the token record of a valid token, and
// records its use (at most once a minute).
func (s *Service) AuthenticateAPIToken(ctx context.Context, token string) (store.APIToken, error) {
	if !strings.HasPrefix(token, APITokenPrefix) {
		return store.APIToken{}, ErrUnauthenticated
	}
	t, err := s.store.APITokenByHash(ctx, hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return t, ErrUnauthenticated
	}
	if err != nil {
		return t, err
	}
	now := s.now()
	if s.shouldTouch(t, now) {
		if err := s.store.TouchAPIToken(ctx, t.ID, now); err == nil {
			at := now.UTC().Truncate(time.Second)
			t.LastUsedAt = &at
		}
	}
	return t, nil
}

func (s *Service) shouldTouch(t store.APIToken, now time.Time) bool {
	s.uses.mu.Lock()
	defer s.uses.mu.Unlock()
	if s.uses.last == nil {
		s.uses.last = map[int64]time.Time{}
	}
	last, ok := s.uses.last[t.ID]
	if !ok && t.LastUsedAt != nil {
		last, ok = *t.LastUsedAt, true
	}
	if ok && now.Sub(last) < touchEvery {
		return false
	}
	s.uses.last[t.ID] = now
	return true
}

// ListAPITokens returns the tokens (never the secret).
func (s *Service) ListAPITokens(ctx context.Context) ([]store.APIToken, error) {
	return s.store.ListAPITokens(ctx)
}

// RevokeAPIToken deletes a token: it stops working at once.
func (s *Service) RevokeAPIToken(ctx context.Context, id int64) error {
	if err := s.store.DeleteAPIToken(ctx, id); err != nil {
		return err
	}
	s.uses.mu.Lock()
	delete(s.uses.last, id)
	s.uses.mu.Unlock()
	return nil
}
