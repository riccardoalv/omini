package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/riccardoalv/omini/internal/auth"
	"github.com/riccardoalv/omini/internal/store"
)

// resetPassword sets a new password for the admin user, for when it was
// forgotten: `omini reset-password [username]`. The new password is read
// from the terminal without echo (twice), or from the first line of stdin
// when it is not a terminal. Every session is signed out.
func resetPassword(ctx context.Context, cfg config, args []string, stdin *os.File, out io.Writer) error {
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "omini.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	username := ""
	if len(args) > 0 {
		username = args[0]
	} else {
		username, err = st.AdminUsername(ctx)
		if errors.Is(err, store.ErrNotFound) {
			return errors.New("no admin user yet: open Omini in a browser to create it")
		}
		if err != nil {
			return err
		}
	}
	password, err := readNewPassword(stdin, out)
	if err != nil {
		return err
	}
	if err := auth.New(st, 0).ResetPassword(ctx, username, password); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no user named %q", username)
		}
		return err
	}
	fmt.Fprintf(out, "Password of %q changed; every session was signed out.\n", username)
	return nil
}

func readNewPassword(stdin *os.File, out io.Writer) (string, error) {
	fd := int(stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no password given on stdin")
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(out, "New password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	fmt.Fprint(out, "Again: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("the passwords do not match")
	}
	return string(first), nil
}
