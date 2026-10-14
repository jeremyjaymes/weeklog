//go:build unix

package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinksAreSkipped(t *testing.T) {
	dir := t.TempDir()
	day := filepath.Join(dir, "sessions", "2026", "10", "08")
	writeSession(t, filepath.Join(day, "rollout-2026-10-08T00-00-00-real-session.jsonl"), sessionLine("real-session", oct5, "Real session"))
	other := filepath.Join(dir, "elsewhere.jsonl")
	writeSession(t, other, sessionLine("elsewhere", oct5, "Linked session"))
	if err := os.Symlink(other, filepath.Join(day, "rollout-2026-10-08T00-00-01-linked.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(day, filepath.Join(dir, "sessions", "2026", "10", "09")); err != nil {
		t.Fatal(err)
	}

	got, _ := collectDir(t, dir, oct5, oct12, nil)
	if len(got) != 1 || got[0].Title != "Real session" || got[0].SourceID != "real-session" {
		t.Fatalf("events = %#v", got)
	}
}
