package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// WorkItemDraft is one unit of work to store.
// Events must already exist. Each event may belong to only one work item,
// and each link must carry a reason.
type WorkItemDraft struct {
	Title  string
	Status string
	Events []WorkItemLink
}

// WorkItemLink attaches one event to a work item.
type WorkItemLink struct {
	EventID int64
	Reason  string
}

// WorkItem is a stored unit of work and the events in it.
// Events are ordered by start time, source, and source id.
type WorkItem struct {
	ID        int64
	Title     string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Events    []WorkItemMember
}

// WorkItemMember is one event in a work item, with the reason it was linked.
type WorkItemMember struct {
	EventID  int64
	Source   string
	SourceID string
	Kind     event.Kind
	Reason   string
}

// ReplaceWorkItems replaces every work item with items.
// The write is one transaction. An empty list clears the table.
// Item ids restart at 1. Events that are not listed are left unlinked.
func (s *Store) ReplaceWorkItems(ctx context.Context, items []WorkItemDraft) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if items == nil {
		items = []WorkItemDraft{}
	}
	seen := make(map[int64]struct{}, len(items))
	for i, item := range items {
		if len(item.Events) == 0 {
			return fmt.Errorf("work item %d has no events", i)
		}
		for _, link := range item.Events {
			if link.EventID == 0 {
				return fmt.Errorf("work item %d has an empty event id", i)
			}
			if strings.TrimSpace(link.Reason) == "" {
				return fmt.Errorf("event %d is missing a link reason", link.EventID)
			}
			if _, ok := seen[link.EventID]; ok {
				return fmt.Errorf("event %d is in two work items", link.EventID)
			}
			seen[link.EventID] = struct{}{}
		}
	}

	now := formatTime(s.nowTime())
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM work_items`); err != nil {
			return fmt.Errorf("replace work items: %w", err)
		}
		for _, item := range items {
			var id int64
			err := tx.QueryRowContext(ctx, `
				INSERT INTO work_items (title, status, created_at, updated_at)
				VALUES (?, ?, ?, ?)
				RETURNING id`,
				item.Title, item.Status, now, now,
			).Scan(&id)
			if err != nil {
				return fmt.Errorf("replace work items: %w", err)
			}
			for _, link := range item.Events {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO work_item_events (work_item_id, event_id, reason)
					VALUES (?, ?, ?)`,
					id, link.EventID, link.Reason,
				); err != nil {
					return fmt.Errorf("replace work items: %w", err)
				}
			}
		}
		return nil
	})
}

// ListWorkItems returns every work item in id order.
func (s *Store) ListWorkItems(ctx context.Context) ([]WorkItem, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id, w.title, w.status, w.created_at, w.updated_at,
			e.id, e.source, e.source_id, e.kind, we.reason
		FROM work_items w
		JOIN work_item_events we ON we.work_item_id = w.id
		JOIN events e ON e.id = we.event_id
		ORDER BY w.id, e.ts, e.source, e.source_id, e.id`)
	if err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []WorkItem
	index := map[int64]int{}
	for rows.Next() {
		var (
			item    WorkItem
			created string
			updated string
			member  WorkItemMember
			kind    string
		)
		if err := rows.Scan(
			&item.ID, &item.Title, &item.Status, &created, &updated,
			&member.EventID, &member.Source, &member.SourceID, &kind, &member.Reason,
		); err != nil {
			return nil, fmt.Errorf("list work items: %w", err)
		}
		item.CreatedAt, err = parseTime(created)
		if err != nil {
			return nil, fmt.Errorf("work item created_at: %w", err)
		}
		item.UpdatedAt, err = parseTime(updated)
		if err != nil {
			return nil, fmt.Errorf("work item updated_at: %w", err)
		}
		member.Kind = event.Kind(kind)
		i, ok := index[item.ID]
		if !ok {
			index[item.ID] = len(out)
			item.Events = []WorkItemMember{member}
			out = append(out, item)
			continue
		}
		out[i].Events = append(out[i].Events, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	if out == nil {
		out = []WorkItem{}
	}
	return out, nil
}
