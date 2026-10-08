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

func TestChangeAndResetPassword(t *testing.T) {
	ctx := context.Background()
	s := newService(t)
	here, _, err := s.Setup(ctx, "admin", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, _, _ := s.Login(ctx, "admin", "correct horse battery")

	if err := s.ChangePassword(ctx, here, "wrong one!", "new password 1"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong current password: %v", err)
	}
	if err := s.ChangePassword(ctx, here, "correct horse battery", "short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("short new password: %v", err)
	}
	if err := s.ChangePassword(ctx, here, "correct horse battery", "new password 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, here); err != nil {
		t.Fatalf("the session that changed it stays signed in: %v", err)
	}
	if _, err := s.Authenticate(ctx, elsewhere); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("other sessions are signed out: %v", err)
	}
	if _, _, err := s.Login(ctx, "admin", "correct horse battery"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}

	// Forgotten: reset from the command line, every session signed out.
	if err := s.ResetPassword(ctx, "admin", "new password 2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, here); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("reset signs out everywhere: %v", err)
	}
	if _, _, err := s.Login(ctx, "admin", "new password 2"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if err := s.ResetPassword(ctx, "nobody", "new password 3"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
}
