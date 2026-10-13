package event

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// Event is one normalized activity record. JSON field names are the schema
// contract for plugins. Optional fields are omitted when empty.
type Event struct {
	Source   string             `json:"source"`
	SourceID string             `json:"source_id"`
	Kind     Kind               `json:"kind"`
	TS       time.Time          `json:"ts"`
	TSEnd    *time.Time         `json:"ts_end,omitempty"`
	Actor    string             `json:"actor,omitempty"`
	Title    string             `json:"title,omitempty"`
	Summary  string             `json:"summary,omitempty"`
	URL      string             `json:"url,omitempty"`
	Repo     string             `json:"repo,omitempty"`
	Branch   string             `json:"branch,omitempty"`
	CWD      string             `json:"cwd,omitempty"`
	Files    []string           `json:"files,omitempty"`
	Refs     []string           `json:"refs,omitempty"`
	State    State              `json:"state,omitempty"`
	Metrics  map[string]float64 `json:"metrics,omitempty"`
	BodyRef  string             `json:"body_ref,omitempty"`
}

// ValidationError is a schema violation on Field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return "invalid event"
	}
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

// Unmarshal decodes a plugin event and prepares it for storage.
// Unknown JSON fields are ignored. Summary text longer than MaxSummaryRunes
// is truncated.
func Unmarshal(data []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(data, &e); err != nil {
		return Event{}, fmt.Errorf("decode event: %w", err)
	}
	prepared, err := Prepare(e)
	if err != nil {
		return Event{}, err
	}
	return prepared, nil
}

// Prepare normalizes e and validates the result.
// The input is not modified. Summary text longer than MaxSummaryRunes is
// truncated. Known kinds and states are canonicalized to lowercase.
func Prepare(e Event) (Event, error) {
	n, err := Normalize(e)
	if err != nil {
		return Event{}, err
	}
	if err := Validate(n); err != nil {
		return Event{}, err
	}
	return n, nil
}

// Normalize returns a cleaned copy of e.
// It trims scalar fields, canonicalizes known kinds and states, converts
// timestamps to UTC, truncates Summary, and drops empty or duplicate files
// and refs while preserving order. Metric keys are trimmed.
func Normalize(e Event) (Event, error) {
	out := e
	out.Source = strings.TrimSpace(e.Source)
	out.SourceID = strings.TrimSpace(e.SourceID)
	out.Kind = canonicalKind(e.Kind)
	out.TS = e.TS.UTC()
	out.TSEnd = nil
	if e.TSEnd != nil && !e.TSEnd.IsZero() {
		end := e.TSEnd.UTC()
		out.TSEnd = &end
	}
	out.Actor = strings.TrimSpace(e.Actor)
	out.Title = strings.TrimSpace(e.Title)
	out.Summary = truncateRunes(e.Summary, MaxSummaryRunes)
	out.URL = strings.TrimSpace(e.URL)
	out.Repo = strings.TrimSpace(e.Repo)
	out.Branch = strings.TrimSpace(e.Branch)
	out.CWD = strings.TrimSpace(e.CWD)
	out.Files = dedupeStrings(e.Files)
	out.Refs = dedupeStrings(e.Refs)
	out.State = canonicalState(e.State)
	out.BodyRef = strings.TrimSpace(e.BodyRef)

	metrics, err := normalizeMetrics(e.Metrics)
	if err != nil {
		return Event{}, err
	}
	out.Metrics = metrics
	return out, nil
}

// Validate reports whether e can be stored.
// It does not truncate or canonicalize. Call Normalize or Prepare first when
// e comes from outside the process. Summary must already be within
// MaxSummaryRunes, and State must already be empty or canonical.
func Validate(e Event) error {
	if e.Source == "" {
		return &ValidationError{Field: "source", Message: "is required"}
	}
	if e.SourceID == "" {
		return &ValidationError{Field: "source_id", Message: "is required"}
	}
	if strings.TrimSpace(string(e.Kind)) == "" {
		return &ValidationError{Field: "kind", Message: "is required"}
	}
	if e.TS.IsZero() {
		return &ValidationError{Field: "ts", Message: "is required"}
	}
	if e.TSEnd != nil && e.TSEnd.Before(e.TS) {
		return &ValidationError{Field: "ts_end", Message: "is before ts"}
	}
	if e.State != "" && !e.State.Known() {
		return &ValidationError{Field: "state", Message: "must be one of open, closed, merged, done, abandoned"}
	}
	if utf8.RuneCountInString(e.Summary) > MaxSummaryRunes {
		return &ValidationError{Field: "summary", Message: fmt.Sprintf("exceeds %d characters", MaxSummaryRunes)}
	}
	for key, value := range e.Metrics {
		if strings.TrimSpace(key) == "" || key != strings.TrimSpace(key) {
			return &ValidationError{Field: "metrics", Message: "key is empty"}
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return &ValidationError{Field: "metrics", Message: fmt.Sprintf("%q is not a finite number", key)}
		}
	}
	return nil
}

func canonicalKind(k Kind) Kind {
	trimmed := strings.TrimSpace(string(k))
	c := Kind(strings.ToLower(trimmed))
	if c.Known() {
		return c
	}
	return Kind(trimmed)
}

func canonicalState(s State) State {
	trimmed := strings.TrimSpace(string(s))
	if trimmed == "" {
		return ""
	}
	c := State(strings.ToLower(trimmed))
	if c.Known() {
		return c
	}
	return State(trimmed)
}

func normalizeMetrics(in map[string]float64) (map[string]float64, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return nil, &ValidationError{Field: "metrics", Message: "key is empty"}
		}
		if _, exists := out[trimmed]; exists {
			return nil, &ValidationError{Field: "metrics", Message: fmt.Sprintf("key %q is duplicated", trimmed)}
		}
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, &ValidationError{Field: "metrics", Message: fmt.Sprintf("%q is not a finite number", trimmed)}
		}
		out[trimmed] = value
	}
	return out, nil
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for r := 0; r < n; r++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}
