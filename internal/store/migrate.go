package store

import (
	"context"
	"fmt"
)

type migration struct {
	version int
	stmts   []string
}

// migrations are applied in order. Add a new entry to extend the schema.
// Do not edit a migration that has already shipped.
var migrations = []migration{
	{version: 1, stmts: []string{
		`CREATE TABLE events (
			id INTEGER PRIMARY KEY,
			source TEXT NOT NULL CHECK (length(source) > 0),
			source_id TEXT NOT NULL CHECK (length(source_id) > 0),
			kind TEXT NOT NULL CHECK (length(kind) > 0),
			ts TEXT NOT NULL,
			ts_end TEXT,
			actor TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			repo TEXT NOT NULL DEFAULT '',
			branch TEXT NOT NULL DEFAULT '',
			cwd TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT '' CHECK (state IN ('', 'open', 'closed', 'merged', 'done', 'abandoned')),
			metrics TEXT NOT NULL DEFAULT '{}' CHECK (json_type(metrics) = 'object'),
			body_ref TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (source, source_id),
			CHECK (ts_end IS NULL OR ts_end >= ts)
		)`,
		`CREATE INDEX idx_events_ts ON events (ts)`,
		`CREATE INDEX idx_events_source_ts ON events (source, ts)`,
		`CREATE INDEX idx_events_repo_ts ON events (repo, ts)`,
		`CREATE TABLE event_files (
			event_id INTEGER NOT NULL REFERENCES events (id) ON DELETE CASCADE,
			path TEXT NOT NULL CHECK (length(path) > 0),
			position INTEGER NOT NULL CHECK (position >= 0),
			PRIMARY KEY (event_id, path)
		)`,
		`CREATE TABLE event_refs (
			event_id INTEGER NOT NULL REFERENCES events (id) ON DELETE CASCADE,
			ref TEXT NOT NULL CHECK (length(ref) > 0),
			position INTEGER NOT NULL CHECK (position >= 0),
			PRIMARY KEY (event_id, ref)
		)`,
		`CREATE INDEX idx_event_refs_ref ON event_refs (ref)`,
		`CREATE TABLE work_items (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE work_item_events (
			work_item_id INTEGER NOT NULL REFERENCES work_items (id) ON DELETE CASCADE,
			event_id INTEGER NOT NULL REFERENCES events (id) ON DELETE CASCADE,
			reason TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (work_item_id, event_id)
		)`,
		// Each event belongs to at most one work item.
		`CREATE UNIQUE INDEX idx_work_item_events_event ON work_item_events (event_id)`,
		`CREATE TABLE source_state (
			source TEXT PRIMARY KEY CHECK (length(source) > 0),
			cursor TEXT NOT NULL DEFAULT '',
			last_run_at TEXT
		)`,
		`CREATE TABLE runs (
			id INTEGER PRIMARY KEY,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			window_since TEXT,
			window_until TEXT,
			source TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (length(status) > 0),
			summary TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX idx_runs_started ON runs (started_at)`,
	}},
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > SchemaVersion {
		return fmt.Errorf("store schema version %d is newer than this binary supports (%d)", current, SchemaVersion)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if m.version != current+1 {
			return fmt.Errorf("missing schema migration %d", current+1)
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("schema v%d: %w", m.version, err)
		}
		current = m.version
	}
	if current != SchemaVersion {
		return fmt.Errorf("store schema version %d, want %d", current, SchemaVersion)
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, m migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range m.stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		m.version, formatTime(s.nowTime()),
	); err != nil {
		return err
	}
	return tx.Commit()
}
