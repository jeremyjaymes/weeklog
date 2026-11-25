package event

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func planExample() Event {
	end := time.Date(2026, 10, 8, 15, 40, 0, 0, time.UTC)
	return Event{
		Source:   "claude-code",
		SourceID: "sess_abc#3",
		Kind:     KindAgentSession,
		TS:       time.Date(2026, 10, 8, 14, 2, 11, 0, time.UTC),
		TSEnd:    &end,
		Actor:    "jeremyjaymes",
		Title:    "Fix Stripe webhook retries",
		Summary:  "first user prompt or the commit subject, truncated to 500 chars",
		URL:      "https://github.com/example/acme-app/pull/12",
		Repo:     "github.com/example/acme-app",
		Branch:   "cursor/fix-webhooks",
		CWD:      "/Users/me/Projects/acme-app",
		Files:    []string{"app/Webhooks.php"},
		Refs:     []string{"GEN-301", "#12", "a1b2c3d"},
		State:    StateMerged,
		Metrics: map[string]float64{
			"tokens_in":   1200,
			"tokens_out":  800,
			"lines_added": 40,
		},
		BodyRef: "file:///tmp/sess.jsonl",
	}
}

func TestPreparePlanExample(t *testing.T) {
	got, err := Prepare(planExample())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	want := planExample()
	if got.Source != want.Source || got.SourceID != want.SourceID || got.Kind != want.Kind {
		t.Fatalf("identity = %s/%s kind %s", got.Source, got.SourceID, got.Kind)
	}
	if !got.TS.Equal(want.TS) || got.TSEnd == nil || !got.TSEnd.Equal(*want.TSEnd) {
		t.Fatalf("ts = %s ts_end = %v", got.TS, got.TSEnd)
	}
	if got.Title != want.Title || got.Summary != want.Summary || got.State != want.State {
		t.Fatalf("text fields changed: %#v", got)
	}
	if got.Metrics["lines_added"] != 40 || got.Metrics["tokens_in"] != 1200 {
		t.Fatalf("metrics = %#v", got.Metrics)
	}
}

func TestValidateRequired(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(Event) Event
		field  string
	}{
		{name: "source", field: "source", mutate: func(e Event) Event { e.Source = "  "; return e }},
		{name: "source_id", field: "source_id", mutate: func(e Event) Event { e.SourceID = ""; return e }},
		{name: "kind", field: "kind", mutate: func(e Event) Event { e.Kind = ""; return e }},
		{name: "ts", field: "ts", mutate: func(e Event) Event { e.TS = time.Time{}; return e }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Prepare(tt.mutate(planExample()))
			var v *ValidationError
			if !errors.As(err, &v) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			if v.Field != tt.field {
				t.Fatalf("field = %q, want %q (%s)", v.Field, tt.field, v)
			}
		})
	}
}

func TestValidateStateAndTime(t *testing.T) {
	e := planExample()
	e.State = "in progress"
	if _, err := Prepare(e); err == nil {
		t.Fatal("expected invalid state")
	}

	e = planExample()
	e.State = ""
	got, err := Prepare(e)
	if err != nil {
		t.Fatalf("empty state: %v", err)
	}
	if got.State != "" {
		t.Fatalf("state = %q", got.State)
	}

	end := e.TS.Add(-time.Second)
	e = planExample()
	e.TSEnd = &end
	if _, err := Prepare(e); err == nil {
		t.Fatal("expected ts_end before ts to fail")
	}

	same := planExample().TS
	e = planExample()
	e.TSEnd = &same
	if _, err := Prepare(e); err != nil {
		t.Fatalf("zero duration: %v", err)
	}
}

func TestPrepareCanonicalKindAndState(t *testing.T) {
	e := planExample()
	e.Kind = " COMMIT "
	e.State = "Merged"
	got, err := Prepare(e)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got.Kind != KindCommit {
		t.Fatalf("kind = %q", got.Kind)
	}
	if got.State != StateMerged {
		t.Fatalf("state = %q", got.State)
	}

	e.Kind = "Note"
	got, err = Prepare(e)
	if err != nil {
		t.Fatalf("unknown kind: %v", err)
	}
	if got.Kind != "Note" || got.Kind.Known() {
		t.Fatalf("kind = %q known=%v", got.Kind, got.Kind.Known())
	}
}

func TestPrepareTruncatesSummary(t *testing.T) {
	e := planExample()
	e.Summary = strings.Repeat("é", MaxSummaryRunes+10)
	got, err := Prepare(e)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if utf8.RuneCountInString(got.Summary) != MaxSummaryRunes {
		t.Fatalf("runes = %d", utf8.RuneCountInString(got.Summary))
	}
	if got.Summary != strings.Repeat("é", MaxSummaryRunes) {
		t.Fatal("summary was not a rune truncation")
	}

	raw := planExample()
	raw.Summary = strings.Repeat("a", MaxSummaryRunes+1)
	if err := Validate(raw); err == nil {
		t.Fatal("Validate accepted an over-long summary")
	}
}

func TestPrepareFilesRefsAndMetrics(t *testing.T) {
	e := planExample()
	e.Files = []string{" b.go ", "a.go", "b.go", " ", "a.go"}
	e.Refs = []string{"GEN-1", "GEN-1", ""}
	e.Metrics = map[string]float64{
		" tokens_in ": 3,
		"lines_added": 0,
	}
	got, err := Prepare(e)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if strings.Join(got.Files, ",") != "b.go,a.go" {
		t.Fatalf("files = %#v", got.Files)
	}
	if strings.Join(got.Refs, ",") != "GEN-1" {
		t.Fatalf("refs = %#v", got.Refs)
	}
	if got.Metrics["tokens_in"] != 3 || got.Metrics["lines_added"] != 0 {
		t.Fatalf("metrics = %#v", got.Metrics)
	}

	e.Metrics = map[string]float64{"a": 1, " a": 2}
	if _, err := Prepare(e); err == nil {
		t.Fatal("expected duplicate metric key")
	}
	e.Metrics = map[string]float64{"tokens": math.NaN()}
	if _, err := Prepare(e); err == nil {
		t.Fatal("expected non-finite metric")
	}
	e.Metrics = map[string]float64{"  ": 1}
	if _, err := Prepare(e); err == nil {
		t.Fatal("expected empty metric key")
	}
}

func TestPrepareDoesNotMutateInput(t *testing.T) {
	e := planExample()
	e.Files = []string{"a.go", "a.go"}
	e.Summary = strings.Repeat("z", MaxSummaryRunes+5)
	filesBefore := strings.Join(e.Files, ",")
	summaryBefore := e.Summary
	if _, err := Prepare(e); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if strings.Join(e.Files, ",") != filesBefore || e.Summary != summaryBefore {
		t.Fatal("Prepare mutated the input")
	}
}

func TestPrepareUTC(t *testing.T) {
	e := planExample()
	cdt := time.FixedZone("CDT", -5*60*60)
	e.TS = time.Date(2026, 10, 8, 9, 2, 11, 123456789, cdt)
	end := time.Date(2026, 10, 8, 10, 40, 0, 0, cdt)
	e.TSEnd = &end
	got, err := Prepare(e)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	wantTS := time.Date(2026, 10, 8, 14, 2, 11, 123456789, time.UTC)
	wantEnd := time.Date(2026, 10, 8, 15, 40, 0, 0, time.UTC)
	if !got.TS.Equal(wantTS) || got.TS.Location() != time.UTC {
		t.Fatalf("ts = %s loc %s", got.TS, got.TS.Location())
	}
	if got.TSEnd == nil || !got.TSEnd.Equal(wantEnd) || got.TSEnd.Location() != time.UTC {
		t.Fatalf("ts_end = %v", got.TSEnd)
	}
}

func TestUnmarshalPlanExample(t *testing.T) {
	const raw = `{
	  "source": "claude-code",
	  "source_id": "sess_abc#3",
	  "kind": "agent_session",
	  "ts": "2026-10-08T14:02:11Z",
	  "ts_end": "2026-10-08T15:40:00Z",
	  "actor": "jeremyjaymes",
	  "title": "Fix Stripe webhook retries",
	  "summary": "first user prompt or the commit subject, truncated to 500 chars",
	  "url": "https://github.com/example/acme-app/pull/12",
	  "repo": "github.com/example/acme-app",
	  "branch": "cursor/fix-webhooks",
	  "cwd": "/Users/me/Projects/acme-app",
	  "files": ["app/Webhooks.php"],
	  "refs": ["GEN-301", "#12", "a1b2c3d"],
	  "state": "merged",
	  "metrics": {"tokens_in": 1200, "tokens_out": 800, "lines_added": 40},
	  "body_ref": "file:///tmp/sess.jsonl",
	  "ignored": true
	}`
	got, err := Unmarshal([]byte(raw))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := planExample()
	if got.Source != want.Source || got.Kind != want.Kind || got.State != want.State {
		t.Fatalf("got %#v", got)
	}
	if !got.TS.Equal(want.TS) || got.TSEnd == nil || !got.TSEnd.Equal(*want.TSEnd) {
		t.Fatalf("times ts=%s ts_end=%v", got.TS, got.TSEnd)
	}
	if strings.Join(got.Files, ",") != "app/Webhooks.php" {
		t.Fatalf("files = %#v", got.Files)
	}
	if strings.Join(got.Refs, ",") != "GEN-301,#12,a1b2c3d" {
		t.Fatalf("refs = %#v", got.Refs)
	}
}

func TestJSONOmitsEmptyOptionalFields(t *testing.T) {
	e := Event{
		Source:   "git",
		SourceID: "abc",
		Kind:     KindCommit,
		TS:       time.Date(2026, 10, 8, 14, 2, 11, 0, time.UTC),
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"ts_end", "actor", "title", "summary", "url", "repo", "branch", "cwd", "files", "refs", "state", "metrics", "body_ref"} {
		if _, ok := m[key]; ok {
			t.Fatalf("optional field %s present in %s", key, b)
		}
	}
	if m["source"] != "git" || m["source_id"] != "abc" || m["kind"] != "commit" {
		t.Fatalf("required fields = %#v", m)
	}
}

func TestKnownKindsAndStates(t *testing.T) {
	if !Kind("Commit").Known() || Kind("note").Known() {
		t.Fatal("kind known check")
	}
	if !State("DONE").Known() || State("").Known() || State("in progress").Known() {
		t.Fatal("state known check")
	}
}
