//go:build unix

package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestOpenPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	path := filepath.Join(dir, "weeklog.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.UpsertEvents(context.Background(), []event.Event{planEvent()}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}

	if got := filePerm(t, dir); got != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", got)
	}
	if got := filePerm(t, path); got != 0o600 {
		t.Fatalf("db mode = %o, want 0600", got)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		got := filePerm(t, path+suffix)
		if got&0o077 != 0 {
			t.Fatalf("%s mode = %o, want no group/other bits", suffix, got)
		}
	}
}

func TestOpenTightensExistingDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weeklog.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.UpsertEvents(context.Background(), []event.Event{planEvent()}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	loosened := false
	for _, sibling := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(sibling); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		if err := os.Chmod(sibling, 0o644); err != nil {
			t.Fatal(err)
		}
		loosened = true
	}
	if !loosened {
		t.Fatal("no store files to loosen")
	}
	if filePerm(t, path)&0o077 == 0 {
		t.Fatal("database was not loosened to 0644")
	}

	reopened, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := filePerm(t, path); got != 0o600 {
		t.Fatalf("db mode = %o, want 0600", got)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		sibling := path + suffix
		if _, err := os.Stat(sibling); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		if got := filePerm(t, sibling); got&0o077 != 0 {
			t.Fatalf("%s mode = %o, want no group/other bits", suffix, got)
		}
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
