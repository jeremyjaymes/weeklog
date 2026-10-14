package linker

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

// TestLinkerFixtures runs the plan's linker scenarios from testdata/linker.
// Each file is an event set plus the work_items snapshot, and each link has
// a because note that says why that reason was stored. These fixtures do not
// include manual overrides. Run applies stored overrides after pass 3.
func TestLinkerFixtures(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/linker/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 8 {
		t.Fatalf("fixtures = %d, want the plan scenarios", len(paths))
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			fx := loadFixture(t, path)
			loc := time.UTC
			if fx.Timezone != "" {
				loc, err = time.LoadLocation(fx.Timezone)
				if err != nil {
					t.Fatal(err)
				}
			}
			if fx.Name == "" || fx.Note == "" {
				t.Fatal("fixture needs a name and a note")
			}
			if fx.AsOf.IsZero() {
				t.Fatal("fixture needs as_of")
			}
			events := make([]store.Record, len(fx.Events))
			seen := map[string]int{}
			for i, ev := range fx.Events {
				if ev.ID == 0 {
					t.Fatalf("event %d has no id", i)
				}
				key := ev.Source + "\x00" + ev.SourceID
				if _, ok := seen[key]; ok {
					t.Fatalf("duplicate event %s/%s", ev.Source, ev.SourceID)
				}
				seen[key] = i
				events[i] = store.Record{ID: ev.ID, Event: ev.Event}
			}
			got := linkEvents(events, []*regexp.Regexp{defaultTicketRE}, true, fx.AsOf, loc)
			assertSnapshot(t, got, fx.WorkItems)
			covered := map[string]int{}
			for _, item := range fx.WorkItems {
				if item.Because == "" {
					t.Fatalf("item %q needs a because note", item.Title)
				}
				for _, ev := range item.Events {
					if ev.Because == "" {
						t.Fatalf("%s/%s needs a because note", ev.Source, ev.SourceID)
					}
					covered[ev.Source+"\x00"+ev.SourceID]++
				}
			}
			if len(covered) != len(seen) {
				t.Fatalf("snapshot covers %d events, fixture has %d", len(covered), len(seen))
			}
			for key, n := range covered {
				if n != 1 {
					t.Fatalf("event %q appears %d times", key, n)
				}
				if _, ok := seen[key]; !ok {
					t.Fatalf("snapshot has unknown event %q", key)
				}
			}
		})
	}
}

type fixture struct {
	Name      string         `json:"name"`
	Note      string         `json:"note"`
	AsOf      time.Time      `json:"as_of"`
	Timezone  string         `json:"timezone,omitempty"`
	Events    []fixtureEvent `json:"events"`
	WorkItems []snapshotItem `json:"work_items"`
}

type fixtureEvent struct {
	ID int64 `json:"id"`
	event.Event
}

type snapshotItem struct {
	Title   string          `json:"title"`
	Status  string          `json:"status"`
	Because string          `json:"because"`
	Events  []snapshotEvent `json:"events"`
}

type snapshotEvent struct {
	Source   string `json:"source"`
	SourceID string `json:"source_id"`
	Reason   string `json:"reason"`
	Because  string `json:"because"`
}

func loadFixture(t *testing.T, path string) fixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var fx fixture
	if err := dec.Decode(&fx); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return fx
}

func assertSnapshot(t *testing.T, got []linkedItem, want []snapshotItem) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("items = %d, want %d\n%s", len(got), len(want), describeItems(got))
	}
	for i := range want {
		if got[i].Title != want[i].Title || got[i].Status != want[i].Status {
			t.Fatalf("item %d = %q [%s], want %q [%s]\n%s",
				i, got[i].Title, got[i].Status, want[i].Title, want[i].Status, describeItems(got))
		}
		if len(got[i].Events) != len(want[i].Events) {
			t.Fatalf("item %d events = %d, want %d\n%s", i, len(got[i].Events), len(want[i].Events), describeItems(got))
		}
		for j := range want[i].Events {
			g := got[i].Events[j]
			w := want[i].Events[j]
			if g.Event.Source != w.Source || g.Event.SourceID != w.SourceID || g.Reason != w.Reason {
				t.Fatalf("item %d event %d = %s/%s %q, want %s/%s %q\n%s",
					i, j, g.Event.Source, g.Event.SourceID, g.Reason, w.Source, w.SourceID, w.Reason, describeItems(got))
			}
			if strings.TrimSpace(g.Reason) == "" {
				t.Fatalf("item %d event %d has an empty reason", i, j)
			}
		}
	}
}
