package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

// noPriorWeekLine is the week-over-week line when the previous ISO week has
// no stored events. A first week and an empty store both land here. The
// counts are not compared with zero, because a missing week is not a week
// of zeros.
const noPriorWeekLine = "No prior week to compare"

// deltaLine compares the ISO week [since, until) with the previous ISO week.
// The empty string means the window is not an ISO week, so there is no
// comparison to print. Counts come from stored events: merged pull requests,
// distinct repository strings, and agent sessions.
func deltaLine(events []store.Record, since, until time.Time, loc *time.Location) string {
	prevSince, prevUntil, ok := previousISOWeek(since, until, loc)
	if !ok {
		return ""
	}
	prior := countWindow(events, prevSince, prevUntil)
	if prior.events == 0 {
		return noPriorWeekLine
	}
	current := countWindow(events, since, until)
	return formatWeekDelta(current, prior)
}

// previousISOWeek reports the ISO week immediately before [since, until).
// ok is false unless that window is one ISO week: Monday at local midnight
// through the next Monday. ISO weeks are Monday weeks, including when
// week_starts is another day.
func previousISOWeek(since, until time.Time, loc *time.Location) (prevSince, prevUntil time.Time, ok bool) {
	if loc == nil {
		loc = time.UTC
	}
	if since.IsZero() || !until.After(since) {
		return time.Time{}, time.Time{}, false
	}
	start := since.In(loc)
	end := until.In(loc)
	if start.Weekday() != time.Monday {
		return time.Time{}, time.Time{}, false
	}
	if start.Hour() != 0 || start.Minute() != 0 || start.Second() != 0 || start.Nanosecond() != 0 {
		return time.Time{}, time.Time{}, false
	}
	if !end.Equal(start.AddDate(0, 0, 7)) {
		return time.Time{}, time.Time{}, false
	}
	return start.AddDate(0, 0, -7), start, true
}

type windowStats struct {
	prsMerged int
	repos     int
	sessions  int
	events    int
}

func countWindow(events []store.Record, since, until time.Time) windowStats {
	var stats windowStats
	repos := map[string]struct{}{}
	for _, ev := range events {
		if !inWindow(ev.TS, since, until) {
			continue
		}
		stats.events++
		switch ev.Kind {
		case event.KindPR:
			if ev.State == event.StateMerged {
				stats.prsMerged++
			}
		case event.KindAgentSession:
			stats.sessions++
		}
		if repo := strings.TrimSpace(ev.Repo); repo != "" {
			repos[repo] = struct{}{}
		}
	}
	stats.repos = len(repos)
	return stats
}

func formatWeekDelta(current, prior windowStats) string {
	parts := []string{
		signedDelta(current.prsMerged-prior.prsMerged, "PR merged", "PRs merged"),
		signedDelta(current.repos-prior.repos, "repo touched", "repos touched"),
		signedDelta(current.sessions-prior.sessions, "session", "sessions"),
	}
	return strings.Join(parts, " · ") + " vs last week"
}

func signedDelta(delta int, one, many string) string {
	n := delta
	mark := "↑"
	switch {
	case delta < 0:
		mark = "↓"
		n = -delta
	case delta == 0:
		mark = "→"
	}
	label := many
	if n == 1 {
		label = one
	}
	return fmt.Sprintf("%s %d %s", mark, n, label)
}
