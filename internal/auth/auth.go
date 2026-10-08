// Package auth implements the single-admin login of the web UI.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/riccardoalv/omini/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAlreadySetUp       = errors.New("an admin user already exists")
	ErrUnauthenticated    = errors.New("not authenticated")
	ErrWrongPassword      = errors.New("the current password is wrong")
	ErrPasswordTooShort   = errors.New("password must have at least 8 characters")
)

// MinPasswordLength is enforced when creating the admin user.
const MinPasswordLength = 8

// BcryptCost is the cost of new password hashes. Tests may lower it to bcrypt.MinCost.
var BcryptCost = bcrypt.DefaultCost

type Service struct {
	store *store.Store
	ttl   time.Duration
	now   func() time.Time
	setup sync.Mutex // serializes first-run setup
}

func New(st *store.Store, sessionTTL time.Duration) *Service {
	if sessionTTL == 0 {
		sessionTTL = 30 * 24 * time.Hour
	}
	return &Service{store: st, ttl: sessionTTL, now: time.Now}
}

// SetupRequired reports whether no admin user exists yet (first run).
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	n, err := s.store.CountUsers(ctx)
	return n == 0, err
}

// Setup creates the admin user on first run and logs it in.
func (s *Service) Setup(ctx context.Context, username, password string) (string, time.Time, error) {
	s.setup.Lock()
	defer s.setup.Unlock()
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	if !required {
		return "", time.Time{}, ErrAlreadySetUp
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return "", time.Time{}, errors.New("username is required")
	}
	if len(password) < MinPasswordLength {
		return "", time.Time{}, ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", time.Time{}, err
	}
	u, err := s.store.CreateUser(ctx, username, string(hash))
	if err != nil {
		return "", time.Time{}, err
	}
	return s.newSession(ctx, u.ID)
}

// Login checks credentials and returns a new session token.
func (s *Service) Login(ctx context.Context, username, password string) (string, time.Time, error) {
	u, err := s.store.GetUserByName(ctx, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		// Same cost as a real check, so response time does not reveal valid usernames.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", time.Time{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}
	return s.newSession(ctx, u.ID)
}

// Authenticate returns the user owning a valid session token.
func (s *Service) Authenticate(ctx context.Context, token string) (store.User, error) {
	if token == "" {
		return store.User{}, ErrUnauthenticated
	}
	u, err := s.store.GetSessionUser(ctx, hashToken(token), s.now())
	if errors.Is(err, store.ErrNotFound) {
		return u, ErrUnauthenticated
	}
	return u, err
}

// ChangePassword sets a new password for the signed-in user after checking
// the current one; every other session of the user is signed out.
func (s *Service) ChangePassword(ctx context.Context, token, current, next string) error {
	u, err := s.Authenticate(ctx, token)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return ErrWrongPassword
	}
	return s.setPassword(ctx, u.ID, next, hashToken(token))
}

// ResetPassword sets a new password for a user without the current one (the
// command line, for a forgotten password) and signs them out everywhere.
func (s *Service) ResetPassword(ctx context.Context, username, next string) error {
	u, err := s.store.GetUserByName(ctx, strings.TrimSpace(username))
	if err != nil {
		return err
	}
	return s.setPassword(ctx, u.ID, next, "")
}

func (s *Service) setPassword(ctx context.Context, userID int64, next, keep string) error {
	if len(next) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), BcryptCost)
	if err != nil {
		return err
	}
	if err := s.store.SetUserPassword(ctx, userID, string(hash)); err != nil {
		return err
	}
	return s.store.DeleteUserSessions(ctx, userID, keep)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, hashToken(token))
}

func (s *Service) newSession(ctx context.Context, userID int64) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	expires := now.Add(s.ttl)
	_ = s.store.DeleteExpiredSessions(ctx, now)
	if err := s.store.CreateSession(ctx, hashToken(token), userID, expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("omini-dummy-password"), bcrypt.DefaultCost)
