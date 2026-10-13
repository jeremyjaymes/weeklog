package store

import (
	"path/filepath"
	"testing"
)

func TestDefaultPathUsesDataHome(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	t.Setenv("XDG_DATA_HOME", dir)
	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "weeklog", "weeklog.db")
	if got != want {
		t.Fatalf("DefaultPath = %s, want %s", got, want)
	}
}
