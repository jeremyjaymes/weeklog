package linker

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestGitBranchFromText(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{text: "git checkout feature/pay", want: "feature/pay"},
		{text: "git checkout -b cursor/fix-webhooks", want: "cursor/fix-webhooks"},
		{text: "git switch -c feature/pay", want: "feature/pay"},
		{text: "git -C /repo checkout -b feature/pay", want: "feature/pay"},
		{text: "git checkout refs/heads/feature/pay", want: "feature/pay"},
		{text: "git checkout main\ngit checkout feature/pay", want: "feature/pay"},
		{text: "Run `git checkout feature/pay` before editing.", want: "feature/pay"},
		{text: "git status", want: ""},
		{text: "git checkout HEAD", want: ""},
		{text: "git checkout " + fullSHA, want: ""},
		{text: "github.com/example/acme-app", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			if got := lastGitBranch(tc.text); got != tc.want {
				t.Fatalf("branch = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFullChainKeepsHardLinksAndStatuses(t *testing.T) {
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
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(4, event.KindCommit, "git", fullSHA, at(8, 15), func(e *event.Event) {
			e.Title = "Fix retries"
			e.Summary = "Fix retries"
			e.Repo = repoOtto
			e.Branch = branchFix
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(5, event.KindPR, "github", "acme/otto#12", at(8, 16), func(e *event.Event) {
			e.Title = "Fix Stripe webhook retries"
			e.Summary = "Closes GEN-301"
			e.Repo = repoOtto
			e.Branch = "refs/heads/" + branchFix
			e.State = event.StateMerged
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
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Fix Stripe webhook retries",
			Status: StatusShipped,
			Events: []linkedEvent{
				linked(events[0], "ref:GEN-301 ticket"),
				linked(events[1], "ref:GEN-301 in commit message, branch:"+branchFix),
				linked(events[2], "branch:"+branchFix),
				linked(events[3], "sha:"+fullSHA+" in pr, branch:"+branchFix),
				linked(events[4], "ref:GEN-301 in body, sha:"+fullSHA+" in pr, branch:"+branchFix+", lineage:acme/otto#12"),
				linked(events[5], "lineage:acme/otto#12"),
			},
		},
		{
			Title:  "unrelated",
			Status: StatusShipped,
			Events: []linkedEvent{
				linked(events[6], "leftover:"+repoOtto+" 2026-10-08"),
			},
		},
	})
}

func TestClosedUnmergedPullRequestIsExplored(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "Spike"
			e.Repo = repoOtto
			e.Branch = "experiment"
		}),
		rec(2, event.KindPR, "github", "acme/otto#9", at(8, 12), func(e *event.Event) {
			e.Title = "Try the spike"
			e.Repo = repoOtto
			e.Branch = "experiment"
			e.State = event.StateClosed
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{{
		Title:  "Try the spike",
		Status: StatusExplored,
		Events: []linkedEvent{
			linked(events[0], "branch:experiment"),
			linked(events[1], "branch:experiment"),
		},
	}})
}

func TestSoftFileOverlapJoinsHardGroup(t *testing.T) {
	end := at(8, 15).Add(40 * time.Minute)
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 12), func(e *event.Event) {
			e.Title = "Fix Stripe webhook retries"
			e.State = event.StateDone
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-1", at(8, 14), func(e *event.Event) {
			e.Title = "look at webhooks"
			e.TSEnd = &end
			e.CWD = cwdOtto
			e.Files = []string{
				"/Users/me/Projects/acme-app/app/Webhooks.php",
				"/Users/me/Projects/acme-app/app/Retry.php",
				"/Users/me/Projects/acme-app/README.md",
			}
		}),
		rec(3, event.KindCommit, "git", fullSHA, at(8, 15).Add(10*time.Minute), func(e *event.Event) {
			e.Title = "Fix retries"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php", "app/Retry.php"}
		}),
		rec(4, event.KindPR, "github", "acme/otto#12", at(8, 16), func(e *event.Event) {
			e.Title = "Fix Stripe webhook retries"
			e.Summary = "Closes GEN-301"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.State = event.StateMerged
			e.Refs = []string{fullSHA}
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{{
		Title:  "Fix Stripe webhook retries",
		Status: StatusShipped,
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket"),
			linked(events[1], "files overlap: 2"),
			linked(events[2], "sha:"+fullSHA+" in pr, branch:feature/pay, files overlap: 2"),
			linked(events[3], "ref:GEN-301 in body, sha:"+fullSHA+" in pr, branch:feature/pay"),
		},
	}})
}

func TestTwoSessionsSplitByFiles(t *testing.T) {
	aEnd := at(8, 12)
	bEnd := at(8, 13)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-a", at(8, 10), func(e *event.Event) {
			e.Title = "Look at webhooks"
			e.TSEnd = &aEnd
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-b", at(8, 11), func(e *event.Event) {
			e.Title = "Look at billing"
			e.TSEnd = &bEnd
			e.CWD = cwdOtto
			e.Files = []string{"app/Billing.php"}
		}),
		rec(3, event.KindCommit, "git", fullSHA, at(8, 12).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Update webhooks"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(4, event.KindCommit, "git", otherSHA, at(8, 13).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Update billing"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
			e.Files = []string{"app/Billing.php"}
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Update webhooks",
			Status: StatusInProgress,
			Events: []linkedEvent{
				linked(events[0], "files overlap: 1"),
				linked(events[2], "files overlap: 1"),
			},
		},
		{
			Title:  "Update billing",
			Status: StatusInProgress,
			Events: []linkedEvent{
				linked(events[1], "files overlap: 1"),
				linked(events[3], "files overlap: 1"),
			},
		},
	})
}

func TestTwoSessionsWithoutFilesStayApart(t *testing.T) {
	aEnd := at(8, 12)
	bEnd := at(8, 13)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-a", at(8, 10), func(e *event.Event) {
			e.Title = "Webhooks spike"
			e.TSEnd = &aEnd
			e.CWD = cwdOtto
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-b", at(8, 11), func(e *event.Event) {
			e.Title = "Billing spike"
			e.TSEnd = &bEnd
			e.CWD = cwdOtto
		}),
		rec(3, event.KindCommit, "git", fullSHA, at(8, 12).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Update webhooks"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
		}),
		rec(4, event.KindCommit, "git", otherSHA, at(8, 13).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Update billing"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Webhooks spike",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[0], "session only")},
		},
		{
			Title:  "Billing spike",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[1], "session only")},
		},
		{
			Title:  "Update webhooks",
			Status: StatusInProgress,
			Events: []linkedEvent{
				linked(events[2], "leftover:"+repoOtto+" 2026-10-08"),
				linked(events[3], "leftover:"+repoOtto+" 2026-10-08"),
			},
		},
	})
}

func TestOnlySessionJoinsUnbranchedCommits(t *testing.T) {
	end := at(8, 11)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
			e.Title = "Poke around"
			e.TSEnd = &end
			e.CWD = cwdOtto
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 11).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "First note"
			e.Repo = repoOtto
			e.CWD = cwdOtto
		}),
		rec(3, event.KindCommit, "git", otherSHA, at(8, 12), func(e *event.Event) {
			e.Title = "Second note"
			e.Repo = repoOtto
			e.CWD = cwdOtto
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{{
		Title:  "First note",
		Status: "",
		Events: []linkedEvent{
			linked(events[0], "only session in window"),
			linked(events[1], "only session in window"),
			linked(events[2], "only session in window"),
		},
	}})
}

func TestHeadBranchJoinsPullRequest(t *testing.T) {
	end := at(8, 15).Add(30 * time.Minute)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 14), func(e *event.Event) {
			e.Title = "Investigate pay"
			e.TSEnd = &end
			e.CWD = cwdOtto
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 15), func(e *event.Event) {
			e.Title = "Handle pay"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
		}),
		rec(3, event.KindPR, "github", "acme/otto#4", at(8, 16), func(e *event.Event) {
			e.Title = "Handle pay"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.State = event.StateOpen
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{{
		Title:  "Handle pay",
		Status: StatusInProgress,
		Events: []linkedEvent{
			linked(events[0], "head branch:feature/pay"),
			linked(events[1], "branch:feature/pay, head branch:feature/pay"),
			linked(events[2], "branch:feature/pay"),
		},
	}})
}

func TestGitCheckoutJoinsBranch(t *testing.T) {
	aEnd := at(8, 15)
	bEnd := at(8, 15)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-pay", at(8, 14), func(e *event.Event) {
			e.Title = "Pay flow"
			e.Summary = "Run git checkout feature/pay before editing"
			e.TSEnd = &aEnd
			e.CWD = cwdOtto
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-look", at(8, 14).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Look around"
			e.Summary = "Look around"
			e.TSEnd = &bEnd
			e.CWD = cwdOtto
		}),
		rec(3, event.KindCommit, "git", fullSHA, at(8, 15).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Handle pay"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
		}),
		rec(4, event.KindPR, "github", "acme/otto#4", at(8, 16), func(e *event.Event) {
			e.Title = "Handle pay"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.State = event.StateOpen
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Handle pay",
			Status: StatusInProgress,
			Events: []linkedEvent{
				linked(events[0], "git branch:feature/pay"),
				linked(events[2], "branch:feature/pay"),
				linked(events[3], "branch:feature/pay, git branch:feature/pay"),
			},
		},
		{
			Title:  "Look around",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[1], "session only")},
		},
	})
}

func TestStoredBranchJoinsCommitsBesideAnotherSession(t *testing.T) {
	aEnd := at(8, 12)
	bEnd := at(8, 12)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-a", at(8, 10), func(e *event.Event) {
			e.Title = "On the branch"
			e.Branch = "feature/pay"
			e.TSEnd = &aEnd
			e.CWD = cwdOtto
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-b", at(8, 11), func(e *event.Event) {
			e.Title = "Just looking"
			e.TSEnd = &bEnd
			e.CWD = cwdOtto
		}),
		rec(3, event.KindCommit, "git", fullSHA, at(8, 12).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Pay commit"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Pay commit",
			Status: StatusInProgress,
			Events: []linkedEvent{
				linked(events[0], "branch:feature/pay"),
				linked(events[2], "branch:feature/pay"),
			},
		},
		{
			Title:  "Just looking",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[1], "session only")},
		},
	})
}

func TestRevertLinksAndAbandons(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "Try Lob adapter"
			e.Repo = repoOtto
			e.Branch = "experiment"
		}),
		rec(2, event.KindCommit, "git", otherSHA, at(8, 12), func(e *event.Event) {
			e.Title = `Revert "Try Lob adapter"`
			e.Summary = "Revert \"Try Lob adapter\"\n\nThis reverts commit " + fullSHA + "."
			e.Repo = repoOtto
			e.Branch = "experiment"
			e.Refs = []string{fullSHA}
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{{
		Title:  "Try Lob adapter",
		Status: StatusAbandoned,
		Events: []linkedEvent{
			linked(events[0], "revert:"+fullSHA),
			linked(events[1], "revert:"+fullSHA),
		},
	}})
}

func TestSessionWithNoCommitsIsExplored(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
			e.Title = "Try Lob adapter spike"
			e.CWD = cwdOtto
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 11), func(e *event.Event) {
			e.Title = "Elsewhere"
			e.Repo = repoOther
			e.Branch = "main"
			e.CWD = "/tmp/other"
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Try Lob adapter spike",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[0], "session only")},
		},
		{
			Title:  "Elsewhere",
			Status: StatusShipped,
			Events: []linkedEvent{linked(events[1], "leftover:"+repoOther+" 2026-10-08")},
		},
	})
}

func TestHardLinkedSessionKeepsHardReasons(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "Fix retries"
			e.Repo = repoOtto
			e.Branch = branchFix
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-1", at(8, 11), func(e *event.Event) {
			e.Title = "look at webhooks"
			e.Branch = branchFix
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(3, event.KindPR, "github", "acme/otto#12", at(8, 12), func(e *event.Event) {
			e.Title = "PR title"
			e.Repo = repoOtto
			e.Branch = branchFix
			e.State = event.StateOpen
		}),
	}
	got := linkAll(events)
	if len(got) != 1 || strings.Contains(got[0].Events[1].Reason, "files overlap:") {
		t.Fatalf("items =\n%s", describeItems(got))
	}
	if got[0].Status != StatusInProgress || got[0].Events[1].Reason != "branch:"+branchFix {
		t.Fatalf("items =\n%s", describeItems(got))
	}
}

func TestCloserSessionWinsSharedFile(t *testing.T) {
	aEnd := at(8, 12)
	bEnd := at(8, 12).Add(20 * time.Minute)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-early", at(8, 10), func(e *event.Event) {
			e.Title = "Early"
			e.TSEnd = &aEnd
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-near", at(8, 12), func(e *event.Event) {
			e.Title = "Near"
			e.TSEnd = &bEnd
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
		rec(3, event.KindCommit, "git", fullSHA, at(8, 12).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "Fix webhooks"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
			e.Files = []string{"app/Webhooks.php"}
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Early",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[0], "session only")},
		},
		{
			Title:  "Fix webhooks",
			Status: StatusInProgress,
			Events: []linkedEvent{
				linked(events[1], "files overlap: 1"),
				linked(events[2], "files overlap: 1"),
			},
		},
	})
}

func TestSessionDoesNotMergeTwoHardGroups(t *testing.T) {
	end := at(8, 16)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 14), func(e *event.Event) {
			e.TSEnd = &end
			e.CWD = cwdOtto
			e.Files = []string{"a.go", "b.go"}
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 15), func(e *event.Event) {
			e.Title = "Change A"
			e.Repo = repoOtto
			e.Branch = "feature-a"
			e.CWD = cwdOtto
			e.Files = []string{"a.go", "b.go"}
		}),
		rec(3, event.KindPR, "github", "acme/otto#1", at(8, 15).Add(10*time.Minute), func(e *event.Event) {
			e.Title = "PR A"
			e.Repo = repoOtto
			e.Branch = "feature-a"
		}),
		rec(4, event.KindCommit, "git", otherSHA, at(8, 15).Add(20*time.Minute), func(e *event.Event) {
			e.Title = "Change B"
			e.Repo = repoOtto
			e.Branch = "feature-b"
			e.CWD = cwdOtto
			e.Files = []string{"a.go"}
		}),
		rec(5, event.KindPR, "github", "acme/otto#2", at(8, 15).Add(30*time.Minute), func(e *event.Event) {
			e.Title = "PR B"
			e.Repo = repoOtto
			e.Branch = "feature-b"
		}),
	}
	got := linkAll(events)
	if len(got) != 2 {
		t.Fatalf("items =\n%s", describeItems(got))
	}
	if got[0].Title != "PR A" || got[1].Title != "PR B" {
		t.Fatalf("items =\n%s", describeItems(got))
	}
	if !strings.Contains(got[0].Events[0].Reason, "files overlap: 2") {
		t.Fatalf("session reason = %s", got[0].Events[0].Reason)
	}
	joined := map[string]bool{}
	for _, item := range got {
		for _, ev := range item.Events {
			joined[ev.Event.SourceID] = true
		}
	}
	if joined["acme/otto#1"] && joined["acme/otto#2"] && got[0].Events[0].Event.SourceID == "sess-1" {
		for _, ev := range got[0].Events {
			if ev.Event.SourceID == "acme/otto#2" {
				t.Fatal("session merged two hard groups")
			}
		}
	}
}

func TestSoftWindowBoundary(t *testing.T) {
	end := time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC)
	inside := time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC)
	outside := time.Date(2026, 10, 8, 13, 0, 1, 0, time.UTC)
	before := time.Date(2026, 10, 8, 9, 59, 59, 0, time.UTC)
	events := []store.Record{
		rec(1, event.KindCommit, "git", strings.Repeat("b", 40), before, func(e *event.Event) {
			e.Title = "too early"
			e.Repo = repoOtto
			e.CWD = cwdOtto
		}),
		rec(2, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
			e.Title = "Working"
			e.TSEnd = &end
			e.CWD = cwdOtto
		}),
		rec(3, event.KindCommit, "git", fullSHA, inside, func(e *event.Event) {
			e.Title = "in window"
			e.Repo = repoOtto
			e.CWD = cwdOtto
		}),
		rec(4, event.KindCommit, "git", otherSHA, outside, func(e *event.Event) {
			e.Title = "too late"
			e.Repo = repoOtto
			e.CWD = cwdOtto
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title: "too early",
			Events: []linkedEvent{
				linked(events[0], "leftover:"+repoOtto+" 2026-10-08"),
				linked(events[3], "leftover:"+repoOtto+" 2026-10-08"),
			},
		},
		{
			Title: "in window",
			Events: []linkedEvent{
				linked(events[1], "only session in window"),
				linked(events[2], "only session in window"),
			},
		},
	})
}

func TestAmbiguousCWDDoesNotSoftLink(t *testing.T) {
	end := at(8, 12)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 8), func(e *event.Event) {
			e.Title = "Which repo"
			e.TSEnd = &end
			e.CWD = cwdOtto
			e.Files = []string{"a.go"}
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 9), func(e *event.Event) {
			e.Title = "Otto"
			e.Repo = repoOtto
			e.Branch = "main"
			e.CWD = cwdOtto
			e.Files = []string{"a.go"}
		}),
		rec(3, event.KindCommit, "git", otherSHA, at(8, 10), func(e *event.Event) {
			e.Title = "Other"
			e.Repo = repoOther
			e.Branch = "main"
			e.CWD = cwdOtto
			e.Files = []string{"a.go"}
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{
		{
			Title:  "Which repo",
			Status: StatusExplored,
			Events: []linkedEvent{linked(events[0], "session only")},
		},
		{
			Title:  "Otto",
			Status: StatusShipped,
			Events: []linkedEvent{linked(events[1], "leftover:"+repoOtto+" 2026-10-08")},
		},
		{
			Title:  "Other",
			Status: StatusShipped,
			Events: []linkedEvent{linked(events[2], "leftover:"+repoOther+" 2026-10-08")},
		},
	})
}

func TestCWDSubdirectoryResolvesRepo(t *testing.T) {
	end := at(8, 11)
	events := []store.Record{
		rec(1, event.KindAgentSession, "claude-code", "sess-1", at(8, 10), func(e *event.Event) {
			e.Title = "Nested"
			e.TSEnd = &end
			e.CWD = cwdOtto + "/app"
			e.Files = []string{"/Users/me/Projects/acme-app/app/a.go"}
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 12), func(e *event.Event) {
			e.Title = "Touch a.go"
			e.Repo = repoOtto
			e.Branch = "feature/pay"
			e.CWD = cwdOtto
			e.Files = []string{"app/a.go"}
		}),
	}
	got := linkAll(events)
	if len(got) != 1 || got[0].Events[0].Reason != "files overlap: 1" {
		t.Fatalf("items =\n%s", describeItems(got))
	}
}

func TestLeftoversSplitByRepoAndLocalDay(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "Otto day"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(2, event.KindCommit, "git", otherSHA, at(9, 10), func(e *event.Event) {
			e.Title = "Otto next"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(3, event.KindCommit, "git", strings.Repeat("c", 40), at(8, 11), func(e *event.Event) {
			e.Title = "Other repo"
			e.Repo = repoOther
			e.Branch = "main"
		}),
	}
	got := linkAll(events)
	if len(got) != 3 {
		t.Fatalf("items =\n%s", describeItems(got))
	}
	if got[0].Title != "Otto day" || got[1].Title != "Other repo" || got[2].Title != "Otto next" {
		t.Fatalf("items =\n%s", describeItems(got))
	}
	if got[0].Events[0].Reason != "leftover:"+repoOtto+" 2026-10-08" {
		t.Fatalf("reason = %s", got[0].Events[0].Reason)
	}
	if got[2].Events[0].Reason != "leftover:"+repoOtto+" 2026-10-09" {
		t.Fatalf("reason = %s", got[2].Events[0].Reason)
	}
}

func TestLeftoverDayUsesLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC), func(e *event.Event) {
			e.Title = "late"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
		rec(2, event.KindCommit, "git", otherSHA, time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), func(e *event.Event) {
			e.Title = "morning"
			e.Repo = repoOtto
			e.Branch = "main"
		}),
	}
	got := linkEvents(events, []*regexp.Regexp{defaultTicketRE}, true, asOf(), loc)
	assertLinked(t, got, []linkedItem{
		{
			Title:  "late",
			Status: StatusShipped,
			Events: []linkedEvent{linked(events[0], "leftover:"+repoOtto+" 2026-10-07")},
		},
		{
			Title:  "morning",
			Status: StatusShipped,
			Events: []linkedEvent{linked(events[1], "leftover:"+repoOtto+" 2026-10-08")},
		},
	})
}

func TestTicketKeyInCommitKeepsStatus(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindTicket, "linear", "GEN-301", at(6, 9), func(e *event.Event) {
			e.Title = "Webhook retries"
			e.State = event.StateDone
		}),
		rec(2, event.KindCommit, "git", fullSHA, at(8, 10), func(e *event.Event) {
			e.Title = "ship it"
			e.Summary = "ship it\n\nThe retry bug is GEN-301."
			e.Repo = repoOtto
			e.Branch = "feature/pay"
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{{
		Title:  "Webhook retries",
		Status: StatusShipped,
		Events: []linkedEvent{
			linked(events[0], "ref:GEN-301 ticket"),
			linked(events[1], "ref:GEN-301 in commit message"),
		},
	}})
}

func TestStaleBranchLeftoverIsAbandoned(t *testing.T) {
	events := []store.Record{
		rec(1, event.KindCommit, "git", fullSHA, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), func(e *event.Event) {
			e.Title = "Old experiment"
			e.Repo = repoOtto
			e.Branch = "experiment"
		}),
	}
	assertLinked(t, linkAll(events), []linkedItem{{
		Title:  "Old experiment",
		Status: StatusAbandoned,
		Events: []linkedEvent{linked(events[0], "leftover:"+repoOtto+" 2026-10-04")},
	}})
}

func linkAll(events []store.Record) []linkedItem {
	return linkEvents(events, []*regexp.Regexp{defaultTicketRE}, true, asOf(), time.UTC)
}

func asOf() time.Time {
	return time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
}

func assertLinked(t *testing.T, got, want []linkedItem) {
	t.Helper()
	assertItems(t, got, want)
	if len(got) != len(want) {
		return
	}
	for i := range want {
		if got[i].Status != want[i].Status {
			t.Fatalf("item %d status = %q, want %q\n%s", i, got[i].Status, want[i].Status, describeItems(got))
		}
	}
}
