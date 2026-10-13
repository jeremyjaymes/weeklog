package timeutil

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Window is a half-open collect range [Since, Until).
// Label is set for an ISO week such as 2026-W41.
type Window struct {
	Since time.Time
	Until time.Time
	Label string
}

// isoWeekPattern matches YYYY-Www. The week number is two digits.
var isoWeekPattern = regexp.MustCompile(`^(\d{4})-W(\d{2})$`)

// ParseISOWeek returns the ISO week named by spec, interpreted in loc.
// spec is YYYY-Www. The week starts Monday at local midnight and ends at
// the next Monday. Years that do not contain the requested week are rejected.
func ParseISOWeek(spec string, loc *time.Location) (Window, error) {
	if loc == nil {
		return Window{}, errors.New("timezone is missing")
	}
	spec = strings.ToUpper(strings.TrimSpace(spec))
	match := isoWeekPattern.FindStringSubmatch(spec)
	if match == nil {
		return Window{}, fmt.Errorf("invalid week %q (use YYYY-Www)", strings.TrimSpace(spec))
	}
	year, err := strconv.Atoi(match[1])
	if err != nil {
		return Window{}, fmt.Errorf("invalid week %q", spec)
	}
	week, err := strconv.Atoi(match[2])
	if err != nil || week < 1 || week > 53 {
		return Window{}, fmt.Errorf("invalid week %q", spec)
	}

	// January 4 is always in ISO week 1. Step back to that week's Monday.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, loc)
	mondayOffset := (int(jan4.Weekday()) + 6) % 7
	week1 := jan4.AddDate(0, 0, -mondayOffset)
	start := week1.AddDate(0, 0, (week-1)*7)
	end := start.AddDate(0, 0, 7)
	isoYear, isoWeek := start.ISOWeek()
	if isoYear != year || isoWeek != week {
		return Window{}, fmt.Errorf("invalid week %q", spec)
	}
	return Window{
		Since: start,
		Until: end,
		Label: fmt.Sprintf("%04d-W%02d", year, week),
	}, nil
}

// PreviousWeek returns the most recently completed week that starts on weekStart.
// The week containing now is excluded, including when now is exactly at that
// week's start. Boundaries are local midnights in now's location.
func PreviousWeek(now time.Time, weekStart time.Weekday) Window {
	loc := now.Location()
	year, month, day := now.In(loc).Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, loc)
	delta := (int(today.Weekday()) - int(weekStart) + 7) % 7
	thisStart := today.AddDate(0, 0, -delta)
	return Window{
		Since: thisStart.AddDate(0, 0, -7),
		Until: thisStart,
	}
}

// ParseRange parses since and until as RFC3339 timestamps or YYYY-MM-DD dates.
// A date is local midnight in loc. until must be after since.
func ParseRange(since, until string, loc *time.Location) (Window, error) {
	if loc == nil {
		return Window{}, errors.New("timezone is missing")
	}
	start, err := parseBoundary(since, loc)
	if err != nil {
		return Window{}, fmt.Errorf("since: %w", err)
	}
	end, err := parseBoundary(until, loc)
	if err != nil {
		return Window{}, fmt.Errorf("until: %w", err)
	}
	if !end.After(start) {
		return Window{}, errors.New("until must be after since")
	}
	return Window{Since: start, Until: end}, nil
}

func parseBoundary(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("must not be empty")
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", value, loc); err == nil {
		return t, nil
	}
	return time.Time{}, errors.New("use RFC3339 or YYYY-MM-DD")
}
