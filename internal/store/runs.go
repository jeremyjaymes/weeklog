package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Run is one collect audit row. A zero FinishedAt, Since, or Until is stored
// as NULL. Source is empty for the collect as a whole.
type Run struct {
	ID         int64
	StartedAt  time.Time
	FinishedAt time.Time
	Since      time.Time
	Until      time.Time
	Source     string
	Status     string
	Summary    string
}

// SourceState is the cursor saved for one configured source.
// Present is false when this source has no saved cursor. An empty Cursor with
// Present true is a cursor the source saved on purpose.
type SourceState struct {
	Cursor  string
	Present bool
	LastRun time.Time
}

// StartRun inserts an unfinished collect audit row and returns its id.
func (s *Store) StartRun(ctx context.Context, source string, since, until time.Time) (int64, error) {
	return s.RecordRun(ctx, Run{
		StartedAt: s.nowTime(),
		Since:     since,
		Until:     until,
		Source:    source,
		Status:    "running",
	})
}

// RecordRun inserts a collect audit row.
func (s *Store) RecordRun(ctx context.Context, run Run) (int64, error) {
	if ctx == nil {
		return 0, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if strings.TrimSpace(run.Status) == "" {
		return 0, errors.New("run status is empty")
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = s.nowTime()
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO runs (started_at, finished_at, window_since, window_until, source, status, summary)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id`,
		formatTime(run.StartedAt),
		nullableTime(&run.FinishedAt),
		nullableTime(&run.Since),
		nullableTime(&run.Until),
		run.Source,
		run.Status,
		run.Summary,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("record run: %w", err)
	}
	return id, nil
}

// FinishRun marks a collect audit row complete.
func (s *Store) FinishRun(ctx context.Context, id int64, status, summary string) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(status) == "" {
		return errors.New("run status is empty")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE runs SET finished_at = ?, status = ?, summary = ? WHERE id = ?`,
		formatTime(s.nowTime()), status, summary, id,
	)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("run %d not found", id)
	}
	return nil
}

// ListRuns returns collect audit rows in insertion order.
func (s *Store) ListRuns(ctx context.Context) ([]Run, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, started_at, finished_at, window_since, window_until, source, status, summary
		FROM runs ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	if out == nil {
		out = []Run{}
	}
	return out, nil
}

// SourceState loads the cursor for source. A missing row has Present false.
func (s *Store) SourceState(ctx context.Context, source string) (SourceState, error) {
	if ctx == nil {
		return SourceState{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return SourceState{}, err
	}
	var (
		cursor string
		last   sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT cursor, last_run_at FROM source_state WHERE source = ?`, source).Scan(&cursor, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceState{}, nil
	}
	if err != nil {
		return SourceState{}, fmt.Errorf("read cursor: %w", err)
	}
	state := SourceState{Cursor: cursor, Present: true}
	if last.Valid {
		parsed, err := parseTime(last.String)
		if err != nil {
			return SourceState{}, fmt.Errorf("read cursor: %w", err)
		}
		state.LastRun = parsed
	}
	return state, nil
}

// SaveCursor records a collect cursor. A nil cursor updates last_run_at when
// a row already exists and does not invent a cursor. A non-nil cursor, including
// an empty string, is stored and sent back on the next collect.
func (s *Store) SaveCursor(ctx context.Context, source string, cursor *string, at time.Time) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(source) == "" {
		return errors.New("source name is empty")
	}
	if at.IsZero() {
		at = s.nowTime()
	}
	stamp := formatTime(at)
	if cursor == nil {
		_, err := s.db.ExecContext(ctx, `UPDATE source_state SET last_run_at = ? WHERE source = ?`, stamp, source)
		if err != nil {
			return fmt.Errorf("save cursor: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO source_state (source, cursor, last_run_at) VALUES (?, ?, ?)
		ON CONFLICT(source) DO UPDATE SET
			cursor = excluded.cursor,
			last_run_at = excluded.last_run_at`,
		source, *cursor, stamp,
	)
	if err != nil {
		return fmt.Errorf("save cursor: %w", err)
	}
	return nil
}

func scanRun(sc rowScanner) (Run, error) {
	var (
		run      Run
		started  string
		finished sql.NullString
		since    sql.NullString
		until    sql.NullString
	)
	if err := sc.Scan(&run.ID, &started, &finished, &since, &until, &run.Source, &run.Status, &run.Summary); err != nil {
		return Run{}, err
	}
	var err error
	run.StartedAt, err = parseTime(started)
	if err != nil {
		return Run{}, fmt.Errorf("run started_at: %w", err)
	}
	run.FinishedAt, err = parseOptionalTime(finished)
	if err != nil {
		return Run{}, fmt.Errorf("run finished_at: %w", err)
	}
	run.Since, err = parseOptionalTime(since)
	if err != nil {
		return Run{}, fmt.Errorf("run window_since: %w", err)
	}
	run.Until, err = parseOptionalTime(until)
	if err != nil {
		return Run{}, fmt.Errorf("run window_until: %w", err)
	}
	return run, nil
}

func parseOptionalTime(value sql.NullString) (time.Time, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, nil
	}
	return parseTime(value.String)
}
