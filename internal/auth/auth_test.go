package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/riccardoalv/omini/internal/store"
)

func init() { BcryptCost = bcrypt.MinCost }

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, time.Hour)
}

func TestFirstRunSetup(t *testing.T) {
	ctx := context.Background()
	s := newService(t)

	if required, _ := s.SetupRequired(ctx); !required {
		t.Fatal("setup should be required on first run")
	}
	if _, _, err := s.Setup(ctx, "admin", "short"); err == nil {
		t.Fatal("short passwords must be rejected")
	}
	token, _, err := s.Setup(ctx, "admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := s.Authenticate(ctx, token); err != nil || u.Username != "admin" {
		t.Fatalf("setup session not valid: %v %v", u, err)
	}
	if _, _, err := s.Setup(ctx, "attacker", "another password"); !errors.Is(err, ErrAlreadySetUp) {
		t.Fatalf("second setup must fail with ErrAlreadySetUp, got %v", err)
	}
}

func TestLoginLogout(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	if _, _, err := s.Setup(ctx, "admin", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ user, pass string }{{"admin", "wrong password"}, {"nobody", "correct horse battery"}} {
		if _, _, err := s.Login(ctx, tc.user, tc.pass); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("login(%q) = %v, want ErrInvalidCredentials", tc.user, err)
		}
	}

	token, _, err := s.Login(ctx, " admin ", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("token must be invalid after logout, got %v", err)
	}
}

func TestSessionsExpire(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	clock := time.Now()
	s.now = func() time.Time { return clock }

	token, _, _ := s.Setup(ctx, "admin", "correct horse battery")
	clock = clock.Add(2 * time.Hour) // TTL is 1h
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestUnknownTokenIsRejected(t *testing.T) {
	s := newService(t)
	for _, token := range []string{"", "made-up"} {
		if _, err := s.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("token %q accepted: %v", token, err)
		}
	}
}
