package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestBackups(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	at := time.Unix(1_790_000_000, 0).UTC()
	add := func(sha string, by bool) bool {
		t.Helper()
		at = at.Add(time.Hour)
		added, err := s.AddBackup(ctx, Backup{NodeID: "dev:fw", IntegrationID: 1, TakenAt: at, Format: "xml", SHA256: sha, Size: 3, ByOmini: by, Content: "sealed-" + sha})
		if err != nil {
			t.Fatal(err)
		}
		return added
	}
	if !add("a", false) || add("a", false) {
		t.Fatal("the same configuration twice is one version")
	}
	if got, _ := s.ChangedOutside(ctx, at.Add(-time.Hour)); len(got) != 0 {
		t.Fatalf("a first version is no change: %v", got)
	}
	if !add("b", true) {
		t.Fatal("a new version")
	}
	if got, _ := s.ChangedOutside(ctx, at.Add(-time.Minute)); len(got) != 0 {
		t.Fatalf("Omini's change: %v", got)
	}
	add("c", false)
	if got, _ := s.ChangedOutside(ctx, at.Add(-time.Minute)); !got["dev:fw"].Equal(at) {
		t.Fatalf("changed outside: %v", got)
	}
	list, err := s.Backups(ctx, "dev:fw")
	if err != nil || len(list) != 3 || list[0].SHA256 != "c" || list[0].Content != "" || !list[1].ByOmini {
		t.Fatalf("list: %+v %v", list, err)
	}
	one, err := s.GetBackup(ctx, list[2].ID)
	if err != nil || one.Content != "sealed-a" || one.Format != "xml" {
		t.Fatalf("one: %+v %v", one, err)
	}
	if _, err := s.GetBackup(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	for i := range BackupsKept + 5 {
		add(fmt.Sprint(i), false)
	}
	if list, _ := s.Backups(ctx, "dev:fw"); len(list) != BackupsKept || list[0].SHA256 != fmt.Sprint(BackupsKept+4) {
		t.Fatalf("kept: %d", len(list))
	}
}
