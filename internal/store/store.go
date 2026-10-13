package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/redact"

	_ "modernc.org/sqlite" // pure Go SQLite driver; builds with CGO_ENABLED=0
)

// SchemaVersion is the migration version this binary applies.
const SchemaVersion = 1

// ErrNotFound is returned when an event row does not exist.
var ErrNotFound = errors.New("event not found")

// Store is a SQLite database of weeklog events and linker tables.
// A Store uses one connection. Methods are safe for concurrent use.
// UpsertEvents redacts string fields before they are written.
type Store struct {
	db       *sql.DB
	now      func() time.Time
	redactor atomic.Pointer[redact.Redactor]
}

var memorySeq atomic.Uint64

// Open opens a SQLite database at path, creating parent directories and
// applying migrations. The path ":memory:" opens a private in-memory database.
// A file-backed store is private to the owner: the directory is created with
// mode 0700, and the database, WAL, and SHM files are mode 0600.
func Open(ctx context.Context, path string) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store path is empty")
	}

	dsn, abs, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}
	if path != ":memory:" {
		info, statErr := os.Stat(abs)
		if statErr == nil && info.IsDir() {
			return nil, fmt.Errorf("store path is a directory: %s", abs)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			return nil, fmt.Errorf("create store directory: %w", err)
		}
		if err := tightenStoreFiles(abs); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	// One connection keeps :memory: databases and write transactions on the
	// same connection. SQLite serializes writers anyway.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open store: %w", err)
	}

	s := &Store{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
	s.redactor.Store(redact.Default())
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.checkForeignKeys(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if path != ":memory:" {
		// Migrations create the WAL and SHM files. Tighten them after SQLite
		// has created them, and again in case an existing file was loose.
		if err := tightenStoreFiles(abs); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return s, nil
}

// tightenStoreFiles makes the database and any WAL/SHM siblings owner-readable
// only. The database file is created with mode 0600 when it does not exist.
func tightenStoreFiles(abs string) error {
	if err := ensurePrivateFile(abs, true); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := ensurePrivateFile(abs+suffix, false); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateFile(path string, create bool) error {
	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat store file: %w", err)
		}
		if !create {
			return nil
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return fmt.Errorf("create store file: %w", err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("create store file: %w", err)
		}
		return nil
	}
	if info.IsDir() || info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("tighten store permissions: %w", err)
	}
	return nil
}

// Close releases the database. It is safe to call more than once.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// Version returns the applied schema migration version.
func (s *Store) Version(ctx context.Context) (int, error) {
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func (s *Store) checkForeignKeys(ctx context.Context) error {
	var enabled int
	if err := s.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		return fmt.Errorf("read foreign_keys: %w", err)
	}
	if enabled != 1 {
		return errors.New("sqlite foreign keys are disabled")
	}
	return nil
}

func sqliteDSN(path string) (dsn string, abs string, err error) {
	pragmas := []string{
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(1)",
	}
	if path == ":memory:" {
		id := memorySeq.Add(1)
		return fmt.Sprintf("file:weeklog%d?mode=memory&%s", id, strings.Join(pragmas, "&")), ":memory:", nil
	}

	abs, err = filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("store path: %w", err)
	}
	pragmas = append(pragmas, "_pragma=journal_mode(WAL)")
	return fileURI(abs) + "?" + strings.Join(pragmas, "&"), abs, nil
}

func fileURI(abs string) string {
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}
