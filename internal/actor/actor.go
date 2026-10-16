package actor

import "sort"

// Built-in actor ids. JSON and actor_evidence use these values.
// Labels are the words printed in the recap and the Markdown report.
// Add a row to catalog, then a trailer, session source, or branch prefix,
// to extend the set.
const (
	Human      = "human"
	Claude     = "claude"
	Codex      = "codex"
	CloudAgent = "cloud_agent"
	Unknown    = "unknown"
)

// Evidence values name the rule that selected the actor.
const (
	EvidenceCoAuthoredBy  = "co-authored-by"
	EvidenceSessionCommit = "session-commit"
	EvidenceBranch        = "branch"
	EvidenceSession       = "session"
	EvidenceHuman         = "human"
	EvidenceConflict      = "conflict"
	EvidenceUnrecognized  = "unrecognized-agent"
	EvidenceUnlinked      = "unlinked"
	EvidenceNone          = "no-evidence"
)

// Result is the actor for one work item and the rule that selected it.
type Result struct {
	Actor    string
	Evidence string
}

// Count is how many work items share an actor.
type Count struct {
	ID    string
	Label string
	N     int
}

type catalogRow struct {
	id    string
	label string
}

// catalog is the display order. Unknown stays last. An id that is not listed
// is still counted; Counts places it after the built-in actors and before
// Unknown, sorted by id.
var catalog = []catalogRow{
	{id: Human, label: "Human"},
	{id: Claude, label: "Claude"},
	{id: Codex, label: "Codex"},
	{id: CloudAgent, label: "Cloud agent"},
	{id: Unknown, label: "Unknown"},
}

// Label is the display name for id. An id outside the catalog is returned
// unchanged so a new actor still renders.
func Label(id string) string {
	if id == "" {
		id = Unknown
	}
	for _, row := range catalog {
		if row.id == id {
			return row.label
		}
	}
	return id
}

// Counts totals ids in catalog order. Empty ids count as Unknown.
// Zero counts are omitted. Ids outside the catalog follow the built-in
// actors, sorted by id, and Unknown is last when its count is greater than zero.
func Counts(ids []string) []Count {
	n := make(map[string]int, len(ids))
	var extra []string
	for _, id := range ids {
		if id == "" {
			id = Unknown
		}
		if n[id] == 0 && !catalogHas(id) {
			extra = append(extra, id)
		}
		n[id]++
	}
	sort.Strings(extra)
	var out []Count
	for _, row := range catalog {
		if row.id == Unknown || n[row.id] == 0 {
			continue
		}
		out = append(out, Count{ID: row.id, Label: row.label, N: n[row.id]})
	}
	for _, id := range extra {
		out = append(out, Count{ID: id, Label: id, N: n[id]})
	}
	if n[Unknown] > 0 {
		out = append(out, Count{ID: Unknown, Label: "Unknown", N: n[Unknown]})
	}
	return out
}

func catalogHas(id string) bool {
	for _, row := range catalog {
		if row.id == id {
			return true
		}
	}
	return false
}
