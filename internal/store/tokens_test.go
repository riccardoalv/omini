package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAPITokens(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Unix(1_800_000_000, 0)
	created, err := s.CreateAPIToken(ctx, "claude", "abc123", now)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Name != "claude" || !created.CreatedAt.Equal(now) || created.LastUsedAt != nil {
		t.Fatalf("created %+v", created)
	}
	if _, err := s.CreateAPIToken(ctx, "again", "abc123", now); err == nil {
		t.Fatal("two tokens cannot share a hash")
	}

	got, err := s.APITokenByHash(ctx, "abc123")
	if err != nil || got.ID != created.ID {
		t.Fatalf("by hash: %+v %v", got, err)
	}
	if _, err := s.APITokenByHash(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown hash is not found: %v", err)
	}

	used := now.Add(time.Hour)
	if err := s.TouchAPIToken(ctx, created.ID, used); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAPITokens(ctx)
	if err != nil || len(list) != 1 || list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(used) {
		t.Fatalf("list %+v %v", list, err)
	}

	if err := s.DeleteAPIToken(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAPIToken(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice: %v", err)
	}
	if _, err := s.APITokenByHash(ctx, "abc123"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a revoked token is gone: %v", err)
	}
	if list, _ := s.ListAPITokens(ctx); len(list) != 0 {
		t.Fatalf("empty list is %v", list)
	}
}
