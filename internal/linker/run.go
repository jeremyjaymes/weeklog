package linker

import (
	"context"
	"errors"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/store"
)

// Options selects ticket patterns for one linker run.
// A nil TicketPatterns uses DefaultTicketPattern. A non-nil empty list
// matches no ticket keys.
type Options struct {
	TicketPatterns []string
}

// Run recomputes work items for every event in st.
// since and until are the collect window. They must be a non-empty range.
// Linking still reads every stored event, so a commit in this window can join
// a ticket that was collected earlier. until is the clock for stale branches,
// and its location chooses the local day for leftovers. The work items in st
// are replaced.
func Run(ctx context.Context, st *store.Store, since, until time.Time) error {
	return RunWith(ctx, st, since, until, Options{})
}

// RunWith is Run with explicit ticket patterns.
func RunWith(ctx context.Context, st *store.Store, since, until time.Time, opt Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if st == nil {
		return errors.New("nil store")
	}
	if since.IsZero() || until.IsZero() || !until.After(since) {
		return errors.New("invalid window")
	}
	matcher, err := compilePatterns(opt.TicketPatterns)
	if err != nil {
		return err
	}
	events, err := st.ListEvents(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	items := linkEvents(events, matcher.res, matcher.deny, until, until.Location())
	drafts := make([]store.WorkItemDraft, len(items))
	for i, item := range items {
		links := make([]store.WorkItemLink, len(item.Events))
		for j, ev := range item.Events {
			links[j] = store.WorkItemLink{EventID: ev.Event.ID, Reason: ev.Reason}
		}
		drafts[i] = store.WorkItemDraft{Title: item.Title, Status: item.Status, Events: links}
	}
	return st.ReplaceWorkItems(ctx, drafts)
}
