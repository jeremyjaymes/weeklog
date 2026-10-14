package linker

import (
	"regexp"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

// Work item statuses. The weekly report groups items by these values.
const (
	StatusShipped    = "shipped"
	StatusInProgress = "in progress"
	StatusExplored   = "explored"
	StatusAbandoned  = "abandoned"
)

// staleAfter is how long a side branch with no pull request stays active.
// The span is 168 hours so a daylight-saving shift does not change the result.
const staleAfter = 7 * 24 * time.Hour

var revertTitle = regexp.MustCompile(`(?i)^revert(?:\s|$)`)

// revertBody captures the SHA in git's default revert message.
var revertBody = regexp.MustCompile(`(?i)\bthis reverts commit\s+([0-9a-f]{7,40})\b`)

// statusOf applies the status rules in order. The first match wins:
// a reverted commit, a merged pull request, a closed or done ticket, a commit
// on the default branch, a side branch with no pull request and no activity
// for 7 days, an open pull request or ticket, a closed unmerged pull request,
// any other side-branch commit, then a session-only item.
func statusOf(events []store.Record, asOf time.Time) string {
	if containsRevert(events) {
		return StatusAbandoned
	}
	if hasPR(events, event.StateMerged) {
		return StatusShipped
	}
	if hasTicket(events, event.StateClosed, event.StateDone) {
		return StatusShipped
	}
	if commitsOnDefault(events) {
		return StatusShipped
	}
	if staleSideBranch(events, asOf) {
		return StatusAbandoned
	}
	if hasPR(events, event.StateOpen) || hasTicket(events, event.StateOpen) {
		return StatusInProgress
	}
	if hasPR(events, event.StateClosed) {
		return StatusExplored
	}
	if commitsOnSideBranch(events) {
		return StatusInProgress
	}
	if onlyAgentSessions(events) {
		return StatusExplored
	}
	return ""
}

func containsRevert(events []store.Record) bool {
	for _, ev := range events {
		if ev.Kind == event.KindCommit && isRevertCommit(ev) {
			return true
		}
	}
	return false
}

func isRevertCommit(ev store.Record) bool {
	if revertTitle.MatchString(strings.TrimSpace(ev.Title)) {
		return true
	}
	return revertBody.MatchString(ev.Summary) || revertBody.MatchString(ev.Title)
}

func revertSHAs(ev store.Record) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(sha string) {
		sha = strings.ToLower(strings.TrimSpace(sha))
		if !shaRef.MatchString(sha) {
			return
		}
		if _, ok := seen[sha]; ok {
			return
		}
		seen[sha] = struct{}{}
		out = append(out, sha)
	}
	for _, text := range []string{ev.Summary, ev.Title} {
		for _, match := range revertBody.FindAllStringSubmatch(text, -1) {
			add(match[1])
		}
	}
	if isRevertCommit(ev) {
		for _, ref := range ev.Refs {
			add(ref)
		}
	}
	return out
}

func hasPR(events []store.Record, states ...event.State) bool {
	return hasKindState(events, event.KindPR, states...)
}

func hasTicket(events []store.Record, states ...event.State) bool {
	return hasKindState(events, event.KindTicket, states...)
}

func hasKindState(events []store.Record, kind event.Kind, states ...event.State) bool {
	for _, ev := range events {
		if ev.Kind != kind {
			continue
		}
		for _, state := range states {
			if strings.EqualFold(string(ev.State), string(state)) {
				return true
			}
		}
	}
	return false
}

func commitsOnDefault(events []store.Record) bool {
	for _, ev := range events {
		if ev.Kind == event.KindCommit && isDefaultBranch(ev.Branch) {
			return true
		}
	}
	return false
}

func commitsOnSideBranch(events []store.Record) bool {
	for _, ev := range events {
		if ev.Kind != event.KindCommit {
			continue
		}
		branch := normBranch(ev.Branch)
		if branch != "" && !isDefaultBranch(branch) {
			return true
		}
	}
	return false
}

func isDefaultBranch(branch string) bool {
	switch strings.ToLower(normBranch(branch)) {
	case "main", "master", "trunk":
		return true
	default:
		return false
	}
}

func staleSideBranch(events []store.Record, asOf time.Time) bool {
	if asOf.IsZero() || !commitsOnSideBranch(events) {
		return false
	}
	for _, ev := range events {
		if ev.Kind == event.KindPR {
			return false
		}
	}
	var latest time.Time
	for _, ev := range events {
		if ev.TS.After(latest) {
			latest = ev.TS
		}
	}
	if latest.IsZero() {
		return false
	}
	return !latest.UTC().After(asOf.UTC().Add(-staleAfter))
}

func onlyAgentSessions(events []store.Record) bool {
	if len(events) == 0 {
		return false
	}
	for _, ev := range events {
		if ev.Kind != event.KindAgentSession {
			return false
		}
	}
	return true
}
