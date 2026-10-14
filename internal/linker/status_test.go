package linker

import (
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestStatusPriority(t *testing.T) {
	asOf := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		events []store.Record
		want   string
	}{
		{
			name: "revert beats a merged pull request and the default branch",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Title = `Revert "Fix"`
					e.Branch = "main"
				}),
				rec(2, event.KindPR, "github", "acme/otto#1", at(8, 12), func(e *event.Event) {
					e.State = event.StateMerged
				}),
			},
			want: StatusAbandoned,
		},
		{
			name: "merged pull request is shipped",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Branch = "feature/pay"
				}),
				rec(2, event.KindPR, "github", "acme/otto#1", at(8, 12), func(e *event.Event) {
					e.State = event.StateMerged
				}),
			},
			want: StatusShipped,
		},
		{
			name: "done ticket is shipped ahead of an open pull request",
			events: []store.Record{
				rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
					e.State = event.StateDone
				}),
				rec(2, event.KindPR, "github", "acme/otto#1", at(8, 12), func(e *event.Event) {
					e.State = event.StateOpen
					e.Branch = "feature/pay"
				}),
			},
			want: StatusShipped,
		},
		{
			name: "closed ticket is shipped",
			events: []store.Record{
				rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
					e.State = event.StateClosed
				}),
			},
			want: StatusShipped,
		},
		{
			name: "default branch is shipped",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(1, 10), func(e *event.Event) {
					e.Branch = "refs/heads/MAIN"
				}),
			},
			want: StatusShipped,
		},
		{
			name: "master and trunk are default branches",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Branch = "refs/remotes/origin/master"
				}),
			},
			want: StatusShipped,
		},
		{
			name: "stale side branch with no pull request is abandoned",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), func(e *event.Event) {
					e.Branch = "experiment"
				}),
			},
			want: StatusAbandoned,
		},
		{
			name: "activity exactly seven days earlier is abandoned",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), func(e *event.Event) {
					e.Branch = "experiment"
				}),
			},
			want: StatusAbandoned,
		},
		{
			name: "activity one second inside seven days stays in progress",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, time.Date(2026, 10, 5, 0, 0, 1, 0, time.UTC), func(e *event.Event) {
					e.Branch = "experiment"
				}),
			},
			want: StatusInProgress,
		},
		{
			name: "open pull request keeps a stale branch in progress",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), func(e *event.Event) {
					e.Branch = "experiment"
				}),
				rec(2, event.KindPR, "github", "acme/otto#1", time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC), func(e *event.Event) {
					e.State = event.StateOpen
					e.Branch = "experiment"
				}),
			},
			want: StatusInProgress,
		},
		{
			name: "open ticket is in progress",
			events: []store.Record{
				rec(1, event.KindTicket, "linear", "GEN-301", at(8, 10), func(e *event.Event) {
					e.State = event.StateOpen
				}),
			},
			want: StatusInProgress,
		},
		{
			name: "closed unmerged pull request is explored",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Branch = "experiment"
				}),
				rec(2, event.KindPR, "github", "acme/otto#1", at(8, 12), func(e *event.Event) {
					e.State = event.StateClosed
					e.Branch = "experiment"
				}),
			},
			want: StatusExplored,
		},
		{
			name: "side branch commits are in progress",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Branch = "feature/pay"
				}),
			},
			want: StatusInProgress,
		},
		{
			name: "sessions only are explored",
			events: []store.Record{
				rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), nil),
			},
			want: StatusExplored,
		},
		{
			name:   "a comment has no status",
			events: []store.Record{rec(1, event.KindComment, "github", "c1", at(8, 10), nil)},
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(tc.events, asOf); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
	// A zero clock does not treat a side branch as stale, so the commit stays in progress.
	var stale []store.Record
	for _, tc := range cases {
		if tc.name == "stale side branch with no pull request is abandoned" {
			stale = tc.events
		}
	}
	if got := statusOf(stale, time.Time{}); got != StatusInProgress {
		t.Fatalf("zero clock status = %q", got)
	}
}
