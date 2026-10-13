package report

import (
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

const (
	statusShipped    = "shipped"
	statusInProgress = "in_progress"
	statusExplored   = "explored"
	statusAbandoned  = "abandoned"

	// staleAfter is how long a branch with no pull request can sit before
	// the item is abandoned. The clock is the end of the report window.
	staleAfter = 7 * 24 * time.Hour
)

func canonicalStatus(stored string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(stored)) {
	case "shipped":
		return statusShipped, true
	case "in_progress", "in progress", "in-progress":
		return statusInProgress, true
	case "explored":
		return statusExplored, true
	case "abandoned", "abandoned / stale", "abandoned/stale", "stale":
		return statusAbandoned, true
	default:
		return "", false
	}
}

func sectionTitle(status string) string {
	switch status {
	case statusShipped:
		return "Shipped"
	case statusInProgress:
		return "In progress"
	case statusExplored:
		return "Explored"
	case statusAbandoned:
		return "Abandoned / stale"
	default:
		return ""
	}
}

// classify picks a section. stored wins when it names a known status.
// until is the exclusive end of the report window.
func classify(events []store.Record, stored string, until time.Time) string {
	if status, ok := canonicalStatus(stored); ok {
		return status
	}
	if len(events) == 0 {
		return statusExplored
	}
	if !onlyReverts(events) && isShipped(events) {
		return statusShipped
	}
	if onlyReverts(events) || isStale(events, until) {
		return statusAbandoned
	}
	if isInProgress(events) {
		return statusInProgress
	}
	if hasAbandonedTicket(events) {
		return statusAbandoned
	}
	return statusExplored
}

func isShipped(events []store.Record) bool {
	for _, ev := range events {
		switch ev.Kind {
		case event.KindPR:
			if ev.State == event.StateMerged {
				return true
			}
		case event.KindTicket:
			if ev.State == event.StateClosed || ev.State == event.StateDone {
				return true
			}
		case event.KindCommit:
			if isDefaultBranch(ev.Branch) && !isRevertCommit(ev) {
				return true
			}
		}
	}
	return false
}

func isInProgress(events []store.Record) bool {
	for _, ev := range events {
		switch ev.Kind {
		case event.KindPR, event.KindTicket:
			if ev.State == event.StateOpen {
				return true
			}
		case event.KindCommit:
			if !isDefaultBranch(ev.Branch) {
				return true
			}
		}
	}
	return false
}

func onlyReverts(events []store.Record) bool {
	n := 0
	for _, ev := range events {
		if ev.Kind != event.KindCommit {
			continue
		}
		n++
		if !isRevertCommit(ev) {
			return false
		}
	}
	return n > 0
}

func isRevertCommit(ev store.Record) bool {
	if ev.Kind != event.KindCommit {
		return false
	}
	title := strings.ToLower(strings.TrimSpace(ev.Title))
	return strings.HasPrefix(title, "revert ") ||
		strings.HasPrefix(title, "revert:") ||
		strings.HasPrefix(title, "revert\"")
}

func isStale(events []store.Record, until time.Time) bool {
	hasPR := false
	hasBranch := false
	var latest time.Time
	for _, ev := range events {
		if ev.Kind == event.KindPR {
			hasPR = true
		}
		if normBranch(ev.Branch) != "" {
			hasBranch = true
		}
		if latest.IsZero() || ev.TS.After(latest) {
			latest = ev.TS
		}
	}
	if hasPR || !hasBranch || latest.IsZero() || until.IsZero() {
		return false
	}
	return !latest.Add(staleAfter).After(until)
}

func hasAbandonedTicket(events []store.Record) bool {
	for _, ev := range events {
		if ev.Kind == event.KindTicket && ev.State == event.StateAbandoned {
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

func normBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if rest, ok := strings.CutPrefix(branch, "refs/remotes/"); ok {
		_, name, found := strings.Cut(rest, "/")
		if !found {
			return ""
		}
		return name
	}
	return branch
}
