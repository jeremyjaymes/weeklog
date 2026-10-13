package source

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestParseCollectDropsInvalidLines(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"event","event":{"source":"todoist","source_id":"e1","kind":"ticket","ts":"2026-10-08T14:02:11Z","title":"one"}}`,
		`not json`,
		`{"type":"event","event":{"source":"other","source_id":"e2","kind":"ticket","ts":"2026-10-08T14:02:11Z"}}`,
		`{"type":"nope"}`,
		``,
		`{"type":"log","level":"warning","msg":"skipped 2 archived projects"}`,
		`{"type":"cursor","cursor":"cur-1"}`,
		`{"type":"event","event":{"source_id":"e3","kind":"ticket","ts":"2026-10-08T15:00:00Z","title":"three"}}`,
		`{"type":"cursor","cursor":"cur-2"}`,
	}, "\n")
	var result Result
	var events []event.Event
	dropped, err := parseCollect("todoist", strings.NewReader(input), &result, &events)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 3 {
		t.Fatalf("dropped = %d", dropped)
	}
	if len(events) != 2 || events[0].SourceID != "e1" || events[1].Source != "todoist" || events[1].SourceID != "e3" {
		t.Fatalf("events = %#v", events)
	}
	if !result.HasCursor || result.Cursor != "cur-2" {
		t.Fatalf("cursor = %q set=%v", result.Cursor, result.HasCursor)
	}
	if len(result.Logs) != 1 || result.Logs[0].Level != "warn" || result.Logs[0].Msg != "skipped 2 archived projects" {
		t.Fatalf("logs = %#v", result.Logs)
	}
}

func TestParseCollectSkipsOversizedLine(t *testing.T) {
	orig := maxMessageBytes
	maxMessageBytes = 200
	t.Cleanup(func() { maxMessageBytes = orig })

	var b strings.Builder
	b.WriteString(strings.Repeat("x", 500))
	b.WriteByte('\n')
	b.WriteString(`{"type":"event","event":{"source_id":"e1","kind":"commit","ts":"2026-10-08T00:00:00Z"}}`)
	b.WriteByte('\n')

	var result Result
	var events []event.Event
	dropped, err := parseCollect("git", strings.NewReader(b.String()), &result, &events)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 || len(events) != 1 || events[0].Source != "git" {
		t.Fatalf("dropped=%d events=%#v", dropped, events)
	}
	if events[0].TS.IsZero() || !events[0].TS.Equal(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("ts = %s", events[0].TS)
	}
}

func TestParseCollectRejectsTooManyEvents(t *testing.T) {
	orig := maxCollectEvents
	maxCollectEvents = 2
	t.Cleanup(func() { maxCollectEvents = orig })

	var b strings.Builder
	for i := 0; i < 4; i++ {
		fmt.Fprintf(&b, "{\"type\":\"event\",\"event\":{\"source_id\":\"e%d\",\"kind\":\"commit\",\"ts\":\"2026-10-08T00:00:00Z\"}}\n", i)
	}
	var result Result
	var events []event.Event
	_, err := parseCollect("git", strings.NewReader(b.String()), &result, &events)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "exceeded 2 events") {
		t.Fatalf("err = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
}
