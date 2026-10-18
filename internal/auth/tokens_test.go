package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAPITokenLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }

	if _, _, err := s.CreateAPIToken(ctx, "  "); !errors.Is(err, ErrTokenName) {
		t.Fatalf("a token needs a name: %v", err)
	}
	if _, _, err := s.CreateAPIToken(ctx, strings.Repeat("x", APITokenNameMax+1)); !errors.Is(err, ErrTokenName) {
		t.Fatalf("a long name is refused: %v", err)
	}
	rec, token, err := s.CreateAPIToken(ctx, " claude ")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "claude" || !strings.HasPrefix(token, APITokenPrefix) {
		t.Fatalf("record %+v token %q", rec, token)
	}
	if raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, APITokenPrefix)); err != nil || len(raw) != 32 {
		t.Fatalf("the token carries 32 random bytes: %d %v", len(raw), err)
	}
	if rec.Hash == token || strings.Contains(rec.Hash, strings.TrimPrefix(token, APITokenPrefix)) {
		t.Fatal("only the hash is stored")
	}

	got, err := s.AuthenticateAPIToken(ctx, token)
	if err != nil || got.ID != rec.ID || got.LastUsedAt == nil {
		t.Fatalf("authenticate: %+v %v", got, err)
	}
	for _, bad := range []string{"", "omini_nope", token[:len(token)-1], strings.TrimPrefix(token, APITokenPrefix)} {
		if _, err := s.AuthenticateAPIToken(ctx, bad); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("%q must be refused: %v", bad, err)
		}
	}

	// The last use is written at most once a minute.
	first := *got.LastUsedAt
	now = now.Add(30 * time.Second)
	if _, err := s.AuthenticateAPIToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListAPITokens(ctx)
	if !list[0].LastUsedAt.Equal(first) {
		t.Fatalf("written again within a minute: %v", list[0].LastUsedAt)
	}
	now = now.Add(time.Minute)
	if _, err := s.AuthenticateAPIToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	list, _ = s.ListAPITokens(ctx)
	if !list[0].LastUsedAt.Equal(now.UTC().Truncate(time.Second)) {
		t.Fatalf("not written after a minute: %v", list[0].LastUsedAt)
	}

	if err := s.RevokeAPIToken(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateAPIToken(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("a revoked token is refused: %v", err)
	}
}
