//go:build unix

package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSymlinksAreSkipped(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "projects", "real")
	writeSession(t, filepath.Join(project, "real-session.jsonl"), sessionLine("real-session", oct5, "Real session"))
	other := filepath.Join(dir, "elsewhere.jsonl")
	writeSession(t, other, sessionLine("elsewhere", oct5, "Linked session"))
	if err := os.Symlink(other, filepath.Join(project, "linked.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, filepath.Join(dir, "projects", "alias")); err != nil {
		t.Fatal(err)
	}

	got, _ := collectDir(t, dir, oct5, oct12, nil)
	if len(got) != 1 || got[0].Title != "Real session" {
		t.Fatalf("events = %#v", titles(got))
	}
}
