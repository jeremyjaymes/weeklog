package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Override kinds stored in the overrides table.
const (
	// OverrideLink joins two events after the automatic passes.
	OverrideLink = "link"
	// OverrideUnlink detaches one event after the automatic passes.
	OverrideUnlink = "unlink"
)

// Override is one user correction. The linker replays these after pass 3,
// in CreatedAt then ID order. EventB is zero for an unlink. The row stores
// event ids only; event text is not copied, so there is nothing to redact.
type Override struct {
	ID        int64
	Kind      string
	EventA    int64
	EventB    int64
	CreatedAt time.Time
}

// AddOverride records a link or unlink correction.
// Both events must already exist. A link names two different events.
// An unlink names one event and leaves EventB zero. The insert is one
// transaction. Each call appends a row; the linker replays the rows in order.
func (s *Store) AddOverride(ctx context.Context, kind string, eventA, eventB int64) (Override, error) {
	if ctx == nil {
		return Override{}, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return Override{}, err
	}
	switch kind {
	case OverrideLink:
		if eventA <= 0 || eventB <= 0 {
			return Override{}, errors.New("link needs two events")
		}
		if eventA == eventB {
			return Override{}, errors.New("cannot link an event to itself")
		}
	case OverrideUnlink:
		if eventA <= 0 {
			return Override{}, errors.New("unlink needs an event")
		}
		if eventB != 0 {
			return Override{}, errors.New("unlink takes one event")
		}
	default:
		return Override{}, fmt.Errorf("unknown override kind %q", kind)
	}

	var out Override
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := eventExists(ctx, tx, eventA); err != nil {
			return fmt.Errorf("event %d: %w", eventA, err)
		}
		if kind == OverrideLink {
			if err := eventExists(ctx, tx, eventB); err != nil {
				return fmt.Errorf("event %d: %w", eventB, err)
			}
		}
		var second any
		if kind == OverrideLink {
			second = eventB
		}
		stamp := formatTime(s.nowTime())
		var created string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO overrides (kind, event_a_id, event_b_id, created_at)
			VALUES (?, ?, ?, ?)
			RETURNING id, created_at`,
			kind, eventA, second, stamp,
		).Scan(&out.ID, &created)
		if err != nil {
			return fmt.Errorf("add override: %w", err)
		}
		parsed, err := parseTime(created)
		if err != nil {
			return fmt.Errorf("override created_at: %w", err)
		}
		out.Kind = kind
		out.EventA = eventA
		out.EventB = eventB
		out.CreatedAt = parsed
		return nil
	})
	if err != nil {
		return Override{}, err
	}
	return out, nil
}

// ListOverrides returns every override in the order the linker applies them.
func (s *Store) ListOverrides(ctx context.Context) ([]Override, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, event_a_id, event_b_id, created_at
		FROM overrides
		ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Override
	for rows.Next() {
		var (
			ov      Override
			eventB  sql.NullInt64
			created string
		)
		if err := rows.Scan(&ov.ID, &ov.Kind, &ov.EventA, &eventB, &created); err != nil {
			return nil, fmt.Errorf("list overrides: %w", err)
		}
		if eventB.Valid {
			ov.EventB = eventB.Int64
		}
		ov.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, fmt.Errorf("override created_at: %w", err)
		}
		out = append(out, ov)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list overrides: %w", err)
	}
	if out == nil {
		out = []Override{}
	}
	return out, nil
}

func eventExists(ctx context.Context, tx *sql.Tx, id int64) error {
	var found int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM events WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
