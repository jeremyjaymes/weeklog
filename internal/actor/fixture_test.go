package actor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

type fixtureCase struct {
	Name         string         `json:"name"`
	Note         string         `json:"note"`
	WantActor    string         `json:"want_actor"`
	WantEvidence string         `json:"want_evidence"`
	Events       []fixtureEvent `json:"events"`
}

type fixtureEvent struct {
	event.Event
	Reason string `json:"reason,omitempty"`
}

func TestFixtures(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "actor", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("fixtures = %d", len(cases))
	}
	seen := map[string]struct{}{}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.Name == "" || tc.Note == "" || tc.WantActor == "" || tc.WantEvidence == "" {
				t.Fatalf("fixture is incomplete: %+v", tc)
			}
			if _, ok := seen[tc.Name]; ok {
				t.Fatalf("duplicate fixture %q", tc.Name)
			}
			seen[tc.Name] = struct{}{}
			events := make([]Event, len(tc.Events))
			for i, ev := range tc.Events {
				events[i] = Event{
					Kind:    ev.Kind,
					Source:  ev.Source,
					Actor:   ev.Actor,
					Title:   ev.Title,
					Summary: ev.Summary,
					Branch:  ev.Branch,
					Reason:  ev.Reason,
				}
			}
			got := Attribute(events)
			if got.Actor != tc.WantActor || got.Evidence != tc.WantEvidence {
				t.Fatalf("actor=%s evidence=%s, want actor=%s evidence=%s\n%s", got.Actor, got.Evidence, tc.WantActor, tc.WantEvidence, tc.Note)
			}
			if Label(got.Actor) == "" {
				t.Fatalf("label for %s is empty", got.Actor)
			}
		})
	}
}

func TestCountsOrder(t *testing.T) {
	got := Counts([]string{Unknown, Codex, "", Claude, CloudAgent, Claude, "aider"})
	want := []Count{
		{ID: Claude, Label: "Claude", N: 2},
		{ID: Codex, Label: "Codex", N: 1},
		{ID: CloudAgent, Label: "Cloud agent", N: 1},
		{ID: "aider", Label: "aider", N: 1},
		{ID: Unknown, Label: "Unknown", N: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("counts = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("counts[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if Label("nope") != "nope" {
		t.Fatalf("label = %s", Label("nope"))
	}
	if len(Counts(nil)) != 0 {
		t.Fatal("empty counts")
	}
}
