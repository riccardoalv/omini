package secret

import (
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	box, err := LoadOrCreate("", filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal("public")
	if err != nil {
		t.Fatal(err)
	}
	if !IsSealed(sealed) || sealed == "public" {
		t.Fatalf("value not sealed: %q", sealed)
	}
	again, _ := box.Seal(sealed)
	if again != sealed {
		t.Fatal("sealing twice must be a no-op")
	}
	plain, err := box.Open(sealed)
	if err != nil || plain != "public" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
}

func TestKeyFileIsReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	a, _ := LoadOrCreate("", path)
	sealed, _ := a.Seal("s3cret")
	b, err := LoadOrCreate("", path)
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := b.Open(sealed); err != nil || plain != "s3cret" {
		t.Fatalf("Open with reloaded key = %q, %v", plain, err)
	}
}

func TestWrongKeyFails(t *testing.T) {
	a, _ := LoadOrCreate("", filepath.Join(t.TempDir(), "a.key"))
	b, _ := LoadOrCreate("", filepath.Join(t.TempDir(), "b.key"))
	sealed, _ := a.Seal("s3cret")
	if _, err := b.Open(sealed); err == nil {
		t.Fatal("expected error opening with another key")
	}
}
