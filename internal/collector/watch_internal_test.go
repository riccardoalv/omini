package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
)

func TestOfflineAfterFollowsTheRoundInterval(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	c := New(st, integration.NewRegistry(), box, Options{Interval: time.Minute})
	if got := c.offlineAfter(ctx); got != 5*time.Minute {
		t.Fatalf("default: %v", got)
	}
	// A device is gone after three rounds, as set in the UI.
	if err := c.SetRoundInterval(ctx, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := c.offlineAfter(ctx); got != 30*time.Minute {
		t.Fatalf("ten-minute rounds: %v", got)
	}
}
