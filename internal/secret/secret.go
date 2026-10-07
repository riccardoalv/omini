// Package secret encrypts sensitive configuration values (passwords, API
// secrets, SNMP communities) before they are written to the database.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const prefix = "enc:v1:"

// Box encrypts and decrypts values with AES-256-GCM.
type Box struct {
	aead cipher.AEAD
}

// LoadOrCreate returns a Box using the key from envKey (base64) when set, or
// from keyFile, creating a new random key file (mode 0600) on first run.
func LoadOrCreate(envKey, keyFile string) (*Box, error) {
	var key []byte
	switch {
	case envKey != "":
		k, err := base64.StdEncoding.DecodeString(envKey)
		if err != nil {
			return nil, fmt.Errorf("decode secret key: %w", err)
		}
		key = k
	default:
		k, err := os.ReadFile(keyFile)
		switch {
		case err == nil:
			key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(k)))
			if err != nil {
				return nil, fmt.Errorf("decode %s: %w", keyFile, err)
			}
		case errors.Is(err, os.ErrNotExist):
			key = make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
				return nil, err
			}
			enc := base64.StdEncoding.EncodeToString(key) + "\n"
			if err := os.WriteFile(keyFile, []byte(enc), 0o600); err != nil {
				return nil, fmt.Errorf("write %s: %w", keyFile, err)
			}
		default:
			return nil, err
		}
	}
	return New(key)
}

// New returns a Box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext. Already sealed values are returned unchanged.
func (b *Box) Seal(plaintext string) (string, error) {
	if IsSealed(plaintext) {
		return plaintext, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal. Plain values are returned unchanged.
func (b *Box) Open(value string) (string, error) {
	if !IsSealed(value) {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secret too short")
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", errors.New("cannot decrypt secret (was the secret key changed?)")
	}
	return string(plain), nil
}

// IsSealed reports whether value was produced by Seal.
func IsSealed(value string) bool { return strings.HasPrefix(value, prefix) }
