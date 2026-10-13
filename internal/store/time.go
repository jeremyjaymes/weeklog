package store

import "time"

// dbTimeLayout is fixed-width so text comparisons match time order.
const dbTimeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTime(t time.Time) string {
	return t.UTC().Format(dbTimeLayout)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(dbTimeLayout, value)
}

func (s *Store) nowTime() time.Time {
	if s == nil || s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}
