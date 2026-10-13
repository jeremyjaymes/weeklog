package linker

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

const (
	fullSHA   = "0123456789abcdef0123456789abcdef01234567"
	otherSHA  = "fedcba9876543210fedcba9876543210fedcba98"
	repoOtto  = "github.com/jeremyjaymes/otto"
	repoOther = "github.com/jeremyjaymes/other"
	cwdOtto   = "/Users/j/Projects/otto"
	branchFix = "cursor/fix-webhooks"
)

func TestPass1SessionCommitPRTicket(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 12), func(e *event.Event) {
			e.Title = "Fix Stripe webhook retries"
		}),
		rec(2, event.KindCommit, "git", otherSHA, at(8, 10), func(e *event.Event) {
			e.Title = "GEN-301 adjust tests"
			e.Summary = "GEN-301 adjust tests"
			e.Repo = repoOtto
			e.Branch = branchFix
			e.CWD = cwdOtto
		}),
		rec(3, event.KindAgentSession, "claude-code", "sess-1", at(8, 14), func(e *event.Event) {
			e.Title = "look at webhooks"
			e.Summary = "look at webhooks"
			e.Branch = branchFix
			e.CWD = cwdOtto
		}),
		rec(4, event.KindCommit, "git", fullSHA, at(8, 15), func(e *event.Event) {
			e.Title = "Fix retries"
			e.Summary = "Fix retries"
			e.Repo = repoOtto
			e.Branch = branchFix
			e.CWD = cwdOtto
		}),
		rec(5, event.KindPR, "github", "acme/otto#12", at(8, 16), func(e *event.Event) {
			e.Title = "Fix Stripe webhook retries"
			e.Summary = "Closes GEN-301"
			e.Repo = repoOtto
			e.Branch = "refs/heads/" + branchFix
			e.Refs = []string{fullSHA, "GEN-301"}
		}),
		rec(6, event.KindPRReview, "github", "acme/otto#12#review-9", at(8, 17), func(e *event.Event) {
			e.Title = "LGTM"
			e.Repo = repoOtto
		}),
		rec(7, event.KindCommit, "git", strings.Repeat("a", 40), at(8, 18), func(e *event.Event) {
			e.Title = "unrelated"
			e.Summary = "unrelated"
			e.Repo = repoOtto
			e.Branch = "main"
			e.CWD = cwdOtto
		}),
	}

	got := linkDefault(events)
	want := []linkedItem{{
		Title: "Fix Stripe webhook retries",
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket"),
			linked(events[1], "ref:GEN-301 in commit message, branch:"+branchFix),
			linked(events[2], "branch:"+branchFix),
			linked(events[3], "sha:"+fullSHA+" in pr, branch:"+branchFix),
			linked(events[4], "ref:GEN-301 in body, sha:"+fullSHA+" in pr, branch:"+branchFix+", lineage:acme/otto#12"),
			linked(events[5], "lineage:acme/otto#12"),
		},
	}}
	assertItems(t, got, want)
}

func TestPass1TicketKeyInPRTitle(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "9b1c4e2a-1111-4111-8111-111111111111", at(6, 9), func(e *event.Event) {
			e.Title = "GEN-301 Admin gate"
		}),
		rec(2, event.KindPR, "github", "acme/otto#8", at(8, 12), func(e *event.Event) {
			e.Title = "Fix GEN-301"
			e.Summary = "No key in the body"
			e.Repo = repoOtto
		}),
	}
	assertItems(t, linkDefault(events), []linkedItem{{
		Title: "GEN-301 Admin gate",
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket"),
			linked(events[1], "ref:GEN-301 in title"),
		},
	}})
}

func TestPass1TicketKeyOnlyInCommitMessage(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Webhook retries"
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "ship it"
			e.Summary = "ship it\n\nThe retry bug is GEN-301."
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(3, event.KindCommit, "git", otherSHA, at(8, 11), func(e *event.Event) {
			e.Title = "ship other"
			e.Summary = "ship other"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
	}
	got := linkDefault(events)
	assertItems(t, got, []linkedItem{{
		Title: "Webhook retries",
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket"),
			linked(events[1], "ref:GEN-301 in commit message"),
		},
	}})
}

func TestPass1TicketKeyInBranchAndRefs(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Admin gate"
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "ship it"
			e.Summary = "ship it"
			e.Repo = repoOtto
			e.Branch = "feature/gen-301-fix"
		}),
		rec(3, event.KindCommit, "git", otherSHA, at(8, 11), func(e *event.Event) {
			e.Title = "mention only in refs"
			e.Summary = "mention only in refs"
			e.Repo = repoOtto
			e.Refs = []string{"GEN-302"}
		}),
		rec(4, event.KindTicket, "linear", "GEN-302", at(8, 12), func(e *event.Event) {
			e.Title = "Other"
		}),
	}
	got := linkDefault(events)
	assertItems(t, got, []linkedItem{
		{
			Title: "Admin gate",
			Events: []linkedEvent{
				linked(events[0], "ref:GEN-301 ticket"),
				linked(events[1], "ref:GEN-301 in branch"),
			},
		},
		{
			Title: "Other",
			Events: []linkedEvent{
				linked(events[2], "ref:GEN-302 in refs"),
				linked(events[3], "ref:GEN-302 ticket"),
			},
		},
	})
}

func TestPass1SHAJoinsCommitAndPR(t *testing.T) {
	t.Run("full and short prefer the longer sha", func(t *testing.T) {
		events := []store.Record{
			rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
				e.Title = "Fix retries"
				e.Repo = repoOtto
				e.Branch = "topic"
			}),
			rec(2, event.KindPR, "github", "acme/otto#4", at(8, 12), func(e *event.Event) {
				e.Title = "PR title"
				e.Repo = repoOther
				e.Branch = "elsewhere"
				e.Refs = []string{fullSHA[:7], fullSHA, "#4"}
			}),
		}
		assertItems(t, linkDefault(events), []linkedItem{{
			Title: "PR title",
			Events: []linkedEvent{
				linked(events[0], "sha:"+fullSHA+" in pr"),
				linked(events[1], "sha:"+fullSHA+" in pr"),
			},
		}})
	})

	t.Run("unique prefix", func(t *testing.T) {
		events := []store.Record{
			rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
				e.Title = "Fix retries"
			}),
			rec(2, event.KindPR, "github", "acme/otto#4", at(8, 12), func(e *event.Event) {
				e.Title = "PR title"
				e.Refs = []string{fullSHA[:7]}
			}),
		}
		assertItems(t, linkDefault(events), []linkedItem{{
			Title: "PR title",
			Events: []linkedEvent{
				linked(events[0], "sha:"+fullSHA[:7]+" in pr"),
				linked(events[1], "sha:"+fullSHA[:7]+" in pr"),
			},
		}})
	})

	t.Run("ambiguous prefix does not join", func(t *testing.T) {
		shaA := fullSHA[:7] + strings.Repeat("a", 33)
		shaB := fullSHA[:7] + strings.Repeat("b", 33)
		events := []store.Record{
			rec(1, event.KindCommit, "git", shaA, at(8, 10), nil),
			rec(2, event.KindCommit, "git", shaB, at(8, 11), nil),
			rec(3, event.KindPR, "github", "acme/otto#4", at(8, 12), func(e *event.Event) {
				e.Refs = []string{fullSHA[:7]}
			}),
		}
		if got := linkDefault(events); len(got) != 0 {
			t.Fatalf("items = %#v", got)
		}
	})
}

func TestPass1BranchJoinsSameRepo(t *testing.T) {
	t.Run("session cwd resolves the repo", func(t *testing.T) {
		events := []store.Record{
			rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
				e.Title = "Fix retries"
				e.Repo = repoOtto
				e.Branch = branchFix
				e.CWD = cwdOtto
			}),
			rec(2, event.KindAgentSession, "claude-code", "sess-1", at(8, 11), func(e *event.Event) {
				e.Title = "look at webhooks"
				e.Branch = branchFix
				e.CWD = cwdOtto
			}),
			rec(3, event.KindPR, "github", "acme/otto#12", at(8, 12), func(e *event.Event) {
				e.Title = "PR title"
				e.Repo = repoOtto
				e.Branch = branchFix
			}),
		}
		assertItems(t, linkDefault(events), []linkedItem{{
			Title: "PR title",
			Events: []linkedEvent{
				linked(events[0], "branch:"+branchFix),
				linked(events[1], "branch:"+branchFix),
				linked(events[2], "branch:"+branchFix),
			},
		}})
	})

	t.Run("remote-tracking branch name", func(t *testing.T) {
		events := []store.Record{
			rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
				e.Title = "Fix retries"
				e.Repo = repoOtto
				e.Branch = "feature"
			}),
			rec(2, event.KindPR, "github", "acme/otto#12", at(8, 12), func(e *event.Event) {
				e.Title = "PR title"
				e.Repo = repoOtto
				e.Branch = "refs/remotes/origin/feature"
			}),
		}
		assertItems(t, linkDefault(events), []linkedItem{{
			Title: "PR title",
			Events: []linkedEvent{
				linked(events[0], "branch:feature"),
				linked(events[1], "branch:feature"),
			},
		}})
	})

	t.Run("same branch in another repo stays apart", func(t *testing.T) {
		events := []store.Record{
			rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
				e.Repo = repoOther
				e.Branch = branchFix
			}),
			rec(2, event.KindPR, "github", "acme/otto#12", at(8, 12), func(e *event.Event) {
				e.Title = "PR title"
				e.Repo = repoOtto
				e.Branch = branchFix
			}),
		}
		if got := linkDefault(events); len(got) != 0 {
			t.Fatalf("items = %#v", got)
		}
	})

	t.Run("ambiguous cwd does not infer a repo", func(t *testing.T) {
		events := []store.Record{
			rec(1, event.KindCommit, "git", fullSHA, at(8, 9), func(e *event.Event) {
				e.Repo = repoOtto
				e.CWD = cwdOtto
				e.Branch = "main"
			}),
			rec(2, event.KindCommit, "git", otherSHA, at(8, 10), func(e *event.Event) {
				e.Repo = repoOther
				e.CWD = cwdOtto
				e.Branch = "main"
			}),
			rec(3, event.KindAgentSession, "claude-code", "sess-1", at(8, 11), func(e *event.Event) {
				e.Branch = branchFix
				e.CWD = cwdOtto
			}),
			rec(4, event.KindPR, "github", "acme/otto#12", at(8, 12), func(e *event.Event) {
				e.Title = "PR title"
				e.Repo = repoOtto
				e.Branch = branchFix
			}),
		}
		if got := linkDefault(events); len(got) != 0 {
			t.Fatalf("items = %#v", got)
		}
	})
}

func TestPass1LineageJoinsReviews(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindPR, "github", "pr-1", at(8, 10), func(e *event.Event) {
			e.Title = "PR title"
		}),
		rec(2, event.KindPRReview, "github", "pr-1-review-9", at(8, 11), nil),
		rec(3, event.KindPRReview, "github", "pr-10", at(8, 12), func(e *event.Event) {
			e.Title = "Other PR"
		}),
		rec(4, event.KindPRReview, "gitlab", "pr-1-review-9", at(8, 13), nil),
	}
	assertItems(t, linkDefault(events), []linkedItem{{
		Title: "PR title",
		Events: []linkedEvent{
			linked(events[0], "lineage:pr-1"),
			linked(events[1], "lineage:pr-1"),
		},
	}})
}

func TestPass1TicketTransitionJoinsKey(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Webhook retries"
		}),
		rec(2, event.KindTicketTransition, "linear", "GEN-301#1", at(8, 9), func(e *event.Event) {
			e.Title = "Started"
			e.Refs = []string{"GEN-301"}
		}),
	}
	assertItems(t, linkDefault(events), []linkedItem{{
		Title: "Webhook retries",
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket, lineage:GEN-301"),
			linked(events[1], "ref:GEN-301 transition, lineage:GEN-301"),
		},
	}})
}

func TestPass1DoesNotMakeSoftLinks(t *testing.T) {
	cases := []struct {
		name   string
		events []store.Record
	}{
		{
			name: "two sessions in one repo",
			events: []store.Record{
				rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
					e.Repo = repoOtto
					e.Branch = branchFix
					e.CWD = cwdOtto
					e.Files = []string{"app/Webhooks.php"}
				}),
				rec(2, event.KindAgentSession, "claude-code", "sess-2", at(8, 12), func(e *event.Event) {
					e.Repo = repoOtto
					e.Branch = branchFix
					e.CWD = cwdOtto
					e.Files = []string{"app/Webhooks.php"}
				}),
			},
		},
		{
			name: "session with no commits",
			events: []store.Record{
				rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
					e.Title = "Try Lob adapter spike"
					e.CWD = cwdOtto
				}),
			},
		},
		{
			name: "file overlap without a hard link",
			events: []store.Record{
				rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
					e.Repo = repoOtto
					e.Files = []string{"app/Webhooks.php"}
				}),
				rec(2, event.KindCommit, "git", fullSHA, at(8, 11), func(e *event.Event) {
					e.Repo = repoOtto
					e.Files = []string{"app/Webhooks.php"}
				}),
			},
		},
		{
			name: "pull request number in a commit",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Summary = "Merge pull request #12"
					e.Refs = []string{"#12"}
				}),
				rec(2, event.KindPR, "github", "acme/otto#12", at(8, 12), func(e *event.Event) {
					e.Title = "PR title"
				}),
			},
		},
		{
			name: "reverted sha mentioned in a commit message",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Title = "Fix retries"
				}),
				rec(2, event.KindCommit, "git", otherSHA, at(8, 12), func(e *event.Event) {
					e.Title = "Revert " + fullSHA
					e.Summary = "Revert " + fullSHA
					e.Refs = []string{fullSHA}
				}),
			},
		},
		{
			name: "one event mentions a ticket",
			events: []store.Record{
				rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Summary = "GEN-301"
				}),
			},
		},
		{
			name: "ISO-8601 is not a ticket",
			events: []store.Record{
				rec(1, event.KindTicket, "linear", "ISO-8601", at(6, 9), func(e *event.Event) {
					e.Title = "Not a ticket"
				}),
				rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Summary = "Parse ISO-8601 timestamps"
				}),
			},
		},
		{
			name: "short project keys are not tickets",
			events: []store.Record{
				rec(1, event.KindTicket, "linear", "A-1", at(6, 9), nil),
				rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
					e.Summary = "A-1"
				}),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := linkDefault(tc.events); len(got) != 0 {
				t.Fatalf("items = %s", describeItems(got))
			}
		})
	}
}

func TestPass1CustomTicketPattern(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Webhook retries"
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Summary = "Fix GEN-301"
		}),
		rec(3, event.KindTicket, "linear", "ABC-12", at(6, 10), func(e *event.Event) {
			e.Title = "Other tracker"
		}),
		rec(4, event.KindCommit, "git", otherSHA, at(8, 11), func(e *event.Event) {
			e.Summary = "Fix ABC-12"
		}),
	}
	got := pass1(events, mustCompile(t, `GEN-\d+`), false)
	assertItems(t, got, []linkedItem{{
		Title: "Webhook retries",
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket"),
			linked(events[1], "ref:GEN-301 in commit message"),
		},
	}})

	// The built-in pattern still links ABC-12, and still skips ISO-8601.
	withISO := append(events,
		rec(5, event.KindTicket, "linear", "ISO-8601", at(6, 11), func(e *event.Event) { e.Title = "Date" }),
		rec(6, event.KindCommit, "git", strings.Repeat("b", 40), at(8, 12), func(e *event.Event) { e.Summary = "ISO-8601" }),
	)
	both := linkDefault(withISO)
	if len(both) != 2 {
		t.Fatalf("default items = %d\n%s", len(both), describeItems(both))
	}
	if both[0].Title != "Webhook retries" || both[1].Title != "Other tracker" {
		t.Fatalf("titles = %q %q", both[0].Title, both[1].Title)
	}
}

func TestPass1PromptAndTitleOrder(t *testing.T) {
	ticket := rec(1, event.KindTicket, "linear", "GEN-301", at(9, 12), func(e *event.Event) {
		e.Title = "Ticket title"
		e.Summary = "GEN-301"
	})
	pr := rec(2, event.KindPR, "github", "acme/otto#3", at(8, 12), func(e *event.Event) {
		e.Title = "PR title"
		e.Summary = "GEN-301"
	})
	common1 := rec(3, event.KindCommit, "git", fullSHA, at(8, 9), func(e *event.Event) {
		e.Title = "Common subject"
		e.Summary = "GEN-301"
	})
	common2 := rec(4, event.KindCommit, "git", otherSHA, at(8, 10), func(e *event.Event) {
		e.Title = "Common subject"
		e.Summary = "GEN-301"
	})
	rare := rec(5, event.KindCommit, "git", strings.Repeat("c", 40), at(8, 8), func(e *event.Event) {
		e.Title = "Rare subject"
		e.Summary = "GEN-301"
	})
	session := rec(6, event.KindAgentSession, "claude-code", "sess-1", at(8, 7), func(e *event.Event) {
		e.Title = "First prompt"
		e.Summary = "GEN-301 in the prompt"
	})

	all := []store.Record{session, rare, common1, common2, pr, ticket}
	if got := linkDefault(all); len(got) != 1 || got[0].Title != "Ticket title" {
		t.Fatalf("title = %s", describeItems(linkDefault(all)))
	}
	if got := linkDefault([]store.Record{session, rare, common1, common2, pr}); len(got) != 1 || got[0].Title != "PR title" {
		t.Fatalf("pr title = %s", describeItems(got))
	}
	if got := linkDefault([]store.Record{session, rare, common1, common2}); len(got) != 1 || got[0].Title != "Common subject" {
		t.Fatalf("commit title = %s", describeItems(got))
	}

	alpha := rec(7, event.KindCommit, "git", fullSHA, at(8, 15), func(e *event.Event) {
		e.Title = "Alpha"
		e.Summary = "GEN-301"
	})
	beta := rec(8, event.KindCommit, "git", otherSHA, at(8, 9), func(e *event.Event) {
		e.Title = "Beta"
		e.Summary = "GEN-301"
	})
	if got := linkDefault([]store.Record{alpha, beta}); len(got) != 1 || got[0].Title != "Beta" {
		t.Fatalf("tie title = %s", describeItems(got))
	}

	blank := rec(9, event.KindAgentSession, "claude-code", "sess-0", at(8, 6), func(e *event.Event) {
		e.Refs = []string{"GEN-301"}
	})
	prose := rec(10, event.KindAgentSession, "claude-code", "sess-2", at(8, 7), func(e *event.Event) {
		e.Summary = "Line one\nGEN-301 more"
	})
	if got := linkDefault([]store.Record{blank, prose}); len(got) != 1 || got[0].Title != "Line one" {
		t.Fatalf("prompt title = %s", describeItems(got))
	}
	if !strings.Contains(linkDefault([]store.Record{blank, prose})[0].Events[1].Reason, "ref:GEN-301 in prompt") {
		t.Fatalf("prompt reason = %s", describeItems(linkDefault([]store.Record{blank, prose})))
	}
}

func TestTicketPatternsFromConfig(t *testing.T) {
	cfg := &config.Config{Sources: []config.Source{
		{Type: "git", Settings: map[string]any{}},
		{Type: "linear", Settings: map[string]any{"ticket_pattern": `GEN-\d+`}},
		{Type: "other", Settings: map[string]any{"ticket_pattern": `GEN-\d+`}},
		{Type: "extra", Settings: map[string]any{"ticket_pattern": `ABC-\d+`}},
		{Type: "bad", Settings: map[string]any{"ticket_pattern": 12}},
		{Type: "blank", Settings: map[string]any{"ticket_pattern": "  "}},
	}}
	got := TicketPatterns(cfg)
	if strings.Join(got, ",") != `GEN-\d+,ABC-\d+` {
		t.Fatalf("patterns = %#v", got)
	}
	if TicketPatterns(nil) != nil {
		t.Fatal("nil config should have no patterns")
	}
}

func rec(id int64, kind event.Kind, source, sourceID string, ts time.Time, edit func(*event.Event)) store.Record {
	ev := event.Event{Source: source, SourceID: sourceID, Kind: kind, TS: ts}
	if edit != nil {
		edit(&ev)
	}
	return store.Record{ID: id, Event: ev}
}

func at(day, hour int) time.Time {
	return time.Date(2026, 10, day, hour, 0, 0, 0, time.UTC)
}

func linked(ev store.Record, reason string) linkedEvent {
	return linkedEvent{Event: ev, Reason: reason}
}

func linkDefault(events []store.Record) []linkedItem {
	return pass1(events, []*regexp.Regexp{defaultTicketRE}, true)
}

func mustCompile(t *testing.T, patterns ...string) []*regexp.Regexp {
	t.Helper()
	out := make([]*regexp.Regexp, len(patterns))
	for i, pattern := range patterns {
		re, err := compilePatterns([]string{pattern})
		if err != nil {
			t.Fatal(err)
		}
		out[i] = re.res[0]
	}
	return out
}

func assertItems(t *testing.T, got, want []linkedItem) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("items = %d, want %d\n%s", len(got), len(want), describeItems(got))
	}
	for i := range want {
		if got[i].Title != want[i].Title {
			t.Fatalf("item %d title = %q, want %q\n%s", i, got[i].Title, want[i].Title, describeItems(got))
		}
		if len(got[i].Events) != len(want[i].Events) {
			t.Fatalf("item %d events = %d, want %d\n%s", i, len(got[i].Events), len(want[i].Events), describeItems(got))
		}
		for j := range want[i].Events {
			g := got[i].Events[j]
			w := want[i].Events[j]
			if g.Event.Source != w.Event.Source || g.Event.SourceID != w.Event.SourceID || g.Reason != w.Reason {
				t.Fatalf("item %d event %d = %s/%s %q, want %s/%s %q\n%s",
					i, j, g.Event.Source, g.Event.SourceID, g.Reason,
					w.Event.Source, w.Event.SourceID, w.Reason, describeItems(got))
			}
			if strings.TrimSpace(g.Reason) == "" {
				t.Fatalf("item %d event %d has an empty reason", i, j)
			}
		}
	}
}

func describeItems(items []linkedItem) string {
	var b strings.Builder
	for i, item := range items {
		b.WriteString(item.Title)
		b.WriteByte('\n')
		for _, ev := range item.Events {
			b.WriteString("  ")
			b.WriteString(ev.Event.Source)
			b.WriteByte('/')
			b.WriteString(ev.Event.SourceID)
			b.WriteString("  ")
			b.WriteString(ev.Reason)
			b.WriteByte('\n')
		}
		if i+1 == len(items) {
			break
		}
	}
	return b.String()
}
