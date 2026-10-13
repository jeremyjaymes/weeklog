package timeutil

import (
	"strings"
	"testing"
	"time"
)

func TestParseISOWeek(t *testing.T) {
	chicago := mustLoc(t, "America/Chicago")
	cases := []struct {
		spec string
		loc  *time.Location
		year int
		mon  time.Month
		day  int
	}{
		{spec: "2026-W41", loc: time.UTC, year: 2026, mon: time.October, day: 5},
		{spec: "2026-w41", loc: time.UTC, year: 2026, mon: time.October, day: 5},
		{spec: "2026-W01", loc: time.UTC, year: 2025, mon: time.December, day: 29},
		{spec: "2020-W53", loc: time.UTC, year: 2020, mon: time.December, day: 28},
		{spec: "2026-W41", loc: chicago, year: 2026, mon: time.October, day: 5},
		{spec: "2026-W10", loc: chicago, year: 2026, mon: time.March, day: 2},
	}
	for _, tc := range cases {
		t.Run(tc.spec+"/"+tc.loc.String(), func(t *testing.T) {
			got, err := ParseISOWeek(tc.spec, tc.loc)
			if err != nil {
				t.Fatal(err)
			}
			if got.Label != strings.ToUpper(tc.spec) {
				t.Fatalf("label = %s", got.Label)
			}
			y, m, d := got.Since.Date()
			if y != tc.year || m != tc.mon || d != tc.day || got.Since.Hour() != 0 || got.Since.Location() != tc.loc {
				t.Fatalf("since = %s", got.Since)
			}
			ey, em, ed := got.Until.Date()
			wantEnd := got.Since.AddDate(0, 0, 7)
			if got.Until.Hour() != 0 || ey != wantEnd.Year() || em != wantEnd.Month() || ed != wantEnd.Day() {
				t.Fatalf("until = %s, want %s", got.Until, wantEnd)
			}
			if !got.Until.After(got.Since) {
				t.Fatalf("window = %s .. %s", got.Since, got.Until)
			}
		})
	}

	// 2026-W10 includes the US spring-forward, so the UTC length is 167 hours
	// while the wall clock still runs Monday midnight to Monday midnight.
	got, err := ParseISOWeek("2026-W10", chicago)
	if err != nil {
		t.Fatal(err)
	}
	if got.Until.Sub(got.Since) != 167*time.Hour {
		t.Fatalf("dst duration = %s", got.Until.Sub(got.Since))
	}
}

func TestParseISOWeekRejects(t *testing.T) {
	for _, spec := range []string{"", "2026W41", "W41", "2026-W00", "2026-W54", "2021-W53", "2026-W1", "week"} {
		if _, err := ParseISOWeek(spec, time.UTC); err == nil {
			t.Fatalf("ParseISOWeek(%q) succeeded", spec)
		}
	}
	if _, err := ParseISOWeek("2026-W41", nil); err == nil {
		t.Fatal("expected a missing timezone to fail")
	}
}

func TestPreviousWeek(t *testing.T) {
	chicago := mustLoc(t, "America/Chicago")
	cases := []struct {
		name   string
		now    time.Time
		start  time.Weekday
		sinceY int
		sinceM time.Month
		sinceD int
		untilY int
		untilM time.Month
		untilD int
		hours  time.Duration
	}{
		{
			name:   "monday utc",
			now:    time.Date(2026, 10, 14, 15, 4, 0, 0, time.UTC),
			start:  time.Monday,
			sinceY: 2026, sinceM: time.October, sinceD: 5,
			untilY: 2026, untilM: time.October, untilD: 12,
			hours: 7 * 24 * time.Hour,
		},
		{
			name:   "exact week start",
			now:    time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
			start:  time.Monday,
			sinceY: 2026, sinceM: time.October, sinceD: 5,
			untilY: 2026, untilM: time.October, untilD: 12,
			hours: 7 * 24 * time.Hour,
		},
		{
			name:   "year boundary",
			now:    time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
			start:  time.Monday,
			sinceY: 2025, sinceM: time.December, sinceD: 22,
			untilY: 2025, untilM: time.December, untilD: 29,
			hours: 7 * 24 * time.Hour,
		},
		{
			name:   "sunday start",
			now:    time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC),
			start:  time.Sunday,
			sinceY: 2026, sinceM: time.October, sinceD: 4,
			untilY: 2026, untilM: time.October, untilD: 11,
			hours: 7 * 24 * time.Hour,
		},
		{
			name:   "chicago dst",
			now:    time.Date(2026, 3, 16, 12, 0, 0, 0, chicago),
			start:  time.Sunday,
			sinceY: 2026, sinceM: time.March, sinceD: 8,
			untilY: 2026, untilM: time.March, untilD: 15,
			hours: 167 * time.Hour,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PreviousWeek(tc.now, tc.start)
			assertMidnight(t, got.Since, tc.sinceY, tc.sinceM, tc.sinceD, tc.now.Location())
			assertMidnight(t, got.Until, tc.untilY, tc.untilM, tc.untilD, tc.now.Location())
			if got.Until.Sub(got.Since) != tc.hours {
				t.Fatalf("duration = %s, want %s", got.Until.Sub(got.Since), tc.hours)
			}
			if got.Label != "" {
				t.Fatalf("label = %q", got.Label)
			}
		})
	}
}

func TestParseRange(t *testing.T) {
	chicago := mustLoc(t, "America/Chicago")
	got, err := ParseRange("2026-10-05", "2026-10-12", chicago)
	if err != nil {
		t.Fatal(err)
	}
	assertMidnight(t, got.Since, 2026, time.October, 5, chicago)
	assertMidnight(t, got.Until, 2026, time.October, 12, chicago)
	if _, off := got.Since.Zone(); off != -5*60*60 {
		t.Fatalf("offset = %d", off)
	}

	got, err = ParseRange("2026-10-05T00:00:00Z", "2026-10-12T00:00:00Z", chicago)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Since.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("since = %s", got.Since)
	}

	if _, err := ParseRange("2026-10-12", "2026-10-05", time.UTC); err == nil {
		t.Fatal("expected reversed range to fail")
	}
	if _, err := ParseRange("2026-10-05", "2026-10-05", time.UTC); err == nil {
		t.Fatal("expected an empty range to fail")
	}
	if _, err := ParseRange("yesterday", "2026-10-12", time.UTC); err == nil {
		t.Fatal("expected a bad since to fail")
	}
	if _, err := ParseRange("2026-10-05", "", time.UTC); err == nil {
		t.Fatal("expected an empty until to fail")
	}
	if _, err := ParseRange("2026-10-05", "2026-10-12", nil); err == nil {
		t.Fatal("expected a missing timezone to fail")
	}
}

func assertMidnight(t *testing.T, got time.Time, year int, month time.Month, day int, loc *time.Location) {
	t.Helper()
	y, m, d := got.Date()
	if y != year || m != month || d != day || got.Hour() != 0 || got.Minute() != 0 || got.Location().String() != loc.String() {
		t.Fatalf("time = %s, want %04d-%02d-%02d midnight %s", got, year, month, day, loc)
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
