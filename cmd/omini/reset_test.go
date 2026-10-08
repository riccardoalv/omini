package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/riccardoalv/omini/internal/auth"
	"github.com/riccardoalv/omini/internal/store"
)

func TestResetPassword(t *testing.T) {
	ctx := context.Background()
	auth.BcryptCost = bcrypt.MinCost
	cfg := config{DataDir: t.TempDir()}
	stdin := func(text string) *os.File {
		f, err := os.CreateTemp(t.TempDir(), "stdin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(text)
		_, _ = f.Seek(0, 0)
		return f
	}
	var out bytes.Buffer
	if err := resetPassword(ctx, cfg, nil, stdin("whatever1\n"), &out); err == nil || !strings.Contains(err.Error(), "no admin user") {
		t.Fatalf("before setup: %v", err)
	}

	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.New(st, 0).Setup(ctx, "admin", "forgotten one"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if err := resetPassword(ctx, cfg, nil, stdin("brand new pw\n"), &out); err != nil {
		t.Fatal(err)
	}
	if err := resetPassword(ctx, cfg, []string{"someone"}, stdin("brand new pw\n"), &out); err == nil {
		t.Fatal("an unknown user must fail")
	}
	st, _ = store.Open(ctx, filepath.Join(cfg.DataDir, "omini.db"))
	defer st.Close()
	if _, _, err := auth.New(st, 0).Login(ctx, "admin", "brand new pw"); err != nil {
		t.Fatalf("login with the reset password: %v", err)
	}
}
