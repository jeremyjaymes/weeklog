// Package store persists weeklog data in SQLite.
//
// The database is opened with modernc.org/sqlite, a pure Go driver that builds
// with CGO_ENABLED=0. The module is pinned to v1.38.2, the newest release that
// still supports Go 1.23. Migrations create events, event_files, event_refs,
// work_items, work_item_events, source_state, and runs. UpsertEvents dedupes
// on (source, source_id), so collecting the same window again does not add rows.
// source_state keeps the cursor from the last successful collect that sent one.
// runs records each collect.
//
// Timestamps are stored in UTC as 2006-01-02T15:04:05.000000000Z so they sort
// lexicographically. String fields are redacted before insert, so the database
// does not keep secrets.
package store
