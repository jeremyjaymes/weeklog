package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// Record is an event row plus store metadata.
type Record struct {
	event.Event
	ID        int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UpsertResult counts rows written by one UpsertEvents call.
// IDs aligns with the input slice.
type UpsertResult struct {
	Inserted int
	Updated  int
	IDs      []int64
}

// UpsertEvents inserts events or updates the row with the same (source, source_id).
// Files and refs are replaced. The write is one transaction: if any event is
// invalid, nothing is written. Collecting the same events again leaves the
// event row count unchanged.
func (s *Store) UpsertEvents(ctx context.Context, events []event.Event) (UpsertResult, error) {
	if ctx == nil {
		return UpsertResult{}, errors.New("nil context")
	}
	prepared := make([]event.Event, len(events))
	for i, ev := range events {
		next, err := event.Prepare(ev)
		if err != nil {
			return UpsertResult{}, fmt.Errorf("event[%d]: %w", i, err)
		}
		prepared[i] = next
	}
	if len(prepared) == 0 {
		return UpsertResult{}, nil
	}

	var result UpsertResult
	result.IDs = make([]int64, len(prepared))
	now := s.nowTime()
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		for i, ev := range prepared {
			id, inserted, err := upsertOne(ctx, tx, ev, now)
			if err != nil {
				return fmt.Errorf("event[%d] %s/%s: %w", i, ev.Source, ev.SourceID, err)
			}
			result.IDs[i] = id
			if inserted {
				result.Inserted++
			} else {
				result.Updated++
			}
		}
		return nil
	})
	if err != nil {
		return UpsertResult{}, err
	}
	return result, nil
}

// GetEvent loads the event with the given dedupe key.
func (s *Store) GetEvent(ctx context.Context, source, sourceID string) (Record, error) {
	var rec Record
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, eventSelect+` WHERE source = ? AND source_id = ?`, source, sourceID)
		var err error
		rec, err = scanEvent(row)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("event %s/%s: %w", source, sourceID, ErrNotFound)
		}
		if err != nil {
			return err
		}
		recs := []Record{rec}
		if err := attachChildren(ctx, tx, recs); err != nil {
			return err
		}
		rec = recs[0]
		return nil
	})
	if err != nil {
		return Record{}, err
	}
	return rec, nil
}

// ListEvents returns every event ordered by start time, source, and source id.
func (s *Store) ListEvents(ctx context.Context) ([]Record, error) {
	var recs []Record
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, eventSelect+` ORDER BY ts, source, source_id, id`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			rec, err := scanEvent(rows)
			if err != nil {
				return err
			}
			recs = append(recs, rec)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return attachChildren(ctx, tx, recs)
	})
	if err != nil {
		return nil, err
	}
	if recs == nil {
		recs = []Record{}
	}
	return recs, nil
}

// CountEvents returns the number of event rows.
func (s *Store) CountEvents(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	return n, nil
}

const eventSelect = `SELECT id, source, source_id, kind, ts, ts_end, actor, title, summary, url, repo, branch, cwd, state, metrics, body_ref, created_at, updated_at FROM events`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEvent(sc rowScanner) (Record, error) {
	var (
		rec     Record
		ts      string
		tsEnd   sql.NullString
		metrics string
		created string
		updated string
		kind    string
		state   string
	)
	err := sc.Scan(
		&rec.ID,
		&rec.Source,
		&rec.SourceID,
		&kind,
		&ts,
		&tsEnd,
		&rec.Actor,
		&rec.Title,
		&rec.Summary,
		&rec.URL,
		&rec.Repo,
		&rec.Branch,
		&rec.CWD,
		&state,
		&metrics,
		&rec.BodyRef,
		&created,
		&updated,
	)
	if err != nil {
		return Record{}, err
	}
	parsed, err := parseTime(ts)
	if err != nil {
		return Record{}, fmt.Errorf("event ts: %w", err)
	}
	rec.TS = parsed
	if tsEnd.Valid {
		end, err := parseTime(tsEnd.String)
		if err != nil {
			return Record{}, fmt.Errorf("event ts_end: %w", err)
		}
		rec.TSEnd = &end
	}
	rec.Kind = event.Kind(kind)
	rec.State = event.State(state)
	rec.Metrics, err = unmarshalMetrics(metrics)
	if err != nil {
		return Record{}, err
	}
	rec.CreatedAt, err = parseTime(created)
	if err != nil {
		return Record{}, fmt.Errorf("event created_at: %w", err)
	}
	rec.UpdatedAt, err = parseTime(updated)
	if err != nil {
		return Record{}, fmt.Errorf("event updated_at: %w", err)
	}
	return rec, nil
}

func upsertOne(ctx context.Context, tx *sql.Tx, e event.Event, now time.Time) (int64, bool, error) {
	metrics, err := marshalMetrics(e.Metrics)
	if err != nil {
		return 0, false, err
	}
	stamp := formatTime(now)
	tsEnd := nullableTime(e.TSEnd)

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE source = ? AND source_id = ?`, e.Source, e.SourceID).Scan(&id)
	inserted := false
	switch {
	case errors.Is(err, sql.ErrNoRows):
		err = tx.QueryRowContext(ctx, `
			INSERT INTO events (
				source, source_id, kind, ts, ts_end, actor, title, summary,
				url, repo, branch, cwd, state, metrics, body_ref, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id`,
			e.Source, e.SourceID, string(e.Kind), formatTime(e.TS), tsEnd,
			e.Actor, e.Title, e.Summary, e.URL, e.Repo, e.Branch, e.CWD,
			string(e.State), metrics, e.BodyRef, stamp, stamp,
		).Scan(&id)
		if err != nil {
			return 0, false, err
		}
		inserted = true
	case err != nil:
		return 0, false, err
	default:
		_, err = tx.ExecContext(ctx, `
			UPDATE events SET
				kind = ?, ts = ?, ts_end = ?, actor = ?, title = ?, summary = ?,
				url = ?, repo = ?, branch = ?, cwd = ?, state = ?, metrics = ?,
				body_ref = ?, updated_at = ?
			WHERE id = ?`,
			string(e.Kind), formatTime(e.TS), tsEnd,
			e.Actor, e.Title, e.Summary, e.URL, e.Repo, e.Branch, e.CWD,
			string(e.State), metrics, e.BodyRef, stamp, id,
		)
		if err != nil {
			return 0, false, err
		}
	}

	if err := replaceChildren(ctx, tx, `event_files`, `path`, id, e.Files); err != nil {
		return 0, false, err
	}
	if err := replaceChildren(ctx, tx, `event_refs`, `ref`, id, e.Refs); err != nil {
		return 0, false, err
	}
	return id, inserted, nil
}

func replaceChildren(ctx context.Context, tx *sql.Tx, table, column string, id int64, values []string) error {
	switch table {
	case "event_files", "event_refs":
	default:
		return fmt.Errorf("unknown child table %s", table)
	}
	switch column {
	case "path", "ref":
	default:
		return fmt.Errorf("unknown child column %s", column)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE event_id = ?`, id); err != nil {
		return err
	}
	insert := `INSERT INTO ` + table + ` (event_id, ` + column + `, position) VALUES (?, ?, ?)`
	for i, value := range values {
		if _, err := tx.ExecContext(ctx, insert, id, value, i); err != nil {
			return err
		}
	}
	return nil
}

func attachChildren(ctx context.Context, tx *sql.Tx, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	index := make(map[int64]int, len(recs))
	args := make([]any, len(recs))
	for i, rec := range recs {
		index[rec.ID] = i
		args[i] = rec.ID
	}
	in := placeholders(len(args))
	if err := loadChild(ctx, tx, `SELECT event_id, path FROM event_files WHERE event_id IN (`+in+`) ORDER BY position, path`, args, index, func(i int, value string) {
		recs[i].Files = append(recs[i].Files, value)
	}); err != nil {
		return err
	}
	return loadChild(ctx, tx, `SELECT event_id, ref FROM event_refs WHERE event_id IN (`+in+`) ORDER BY position, ref`, args, index, func(i int, value string) {
		recs[i].Refs = append(recs[i].Refs, value)
	})
}

func loadChild(ctx context.Context, tx *sql.Tx, query string, args []any, index map[int64]int, add func(i int, value string)) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			return err
		}
		i, ok := index[id]
		if !ok {
			continue
		}
		add(i, value)
	}
	return rows.Err()
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('?')
	}
	return b.String()
}

func nullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return formatTime(*t)
}

func marshalMetrics(m map[string]float64) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("encode metrics: %w", err)
	}
	return string(b), nil
}

func unmarshalMetrics(s string) (map[string]float64, error) {
	if s == "" || s == "{}" || s == "null" {
		return nil, nil
	}
	var m map[string]float64
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("decode metrics: %w", err)
	}
	if len(m) == 0 {
		return nil, nil
	}
	return m, nil
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
