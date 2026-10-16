package cursorcloud

import (
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// prPath finds a pull or merge request number in a URL path.
var prPath = regexp.MustCompile(`/(?:pull|pulls|merge_requests)/(\d+)\b`)

type agent struct {
	ID        string
	Name      string
	URL       string
	UpdatedAt time.Time
}

type run struct {
	ID        string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
	HasUpdate bool
	Branches  []branch
}

type branch struct {
	RepoURL string
	Name    string
	PRURL   string
}

// sessionEvent maps one kept run. Summary is never set: the assistant
// result stays on Cursor's side.
func sessionEvent(agent agent, run run, actor string) (event.Event, bool) {
	if !safeID(agent.ID) || !safeID(run.ID) || run.CreatedAt.IsZero() {
		return event.Event{}, false
	}
	ev := event.Event{
		Source:   sourceName,
		SourceID: agent.ID + "/" + run.ID,
		Kind:     event.KindAgentSession,
		TS:       run.CreatedAt.UTC(),
		Actor:    actor,
		Title:    oneLine(agent.Name),
		URL:      runURL(agent, run),
		Repo:     firstRepo(run),
		Branch:   firstBranch(run),
		Refs:     runRefs(run),
		State:    runState(run.Status),
	}
	if run.HasUpdate && !run.UpdatedAt.Before(run.CreatedAt) {
		end := run.UpdatedAt.UTC()
		ev.TSEnd = &end
	}
	return ev, true
}

func runState(status string) event.State {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "CREATING", "RUNNING":
		return event.StateOpen
	case "FINISHED":
		return event.StateDone
	default:
		return event.StateAbandoned
	}
}

func runURL(agent agent, run run) string {
	if len(run.Branches) > 0 {
		if pr := strings.TrimSpace(run.Branches[0].PRURL); pr != "" {
			return pr
		}
	}
	return strings.TrimSpace(agent.URL)
}

func firstRepo(run run) string {
	if len(run.Branches) == 0 {
		return ""
	}
	return strings.TrimSpace(run.Branches[0].RepoURL)
}

func firstBranch(run run) string {
	if len(run.Branches) == 0 {
		return ""
	}
	return strings.TrimSpace(run.Branches[0].Name)
}

func runRefs(run run) []string {
	seen := map[string]struct{}{}
	var refs []string
	for _, b := range run.Branches {
		if ref, ok := prRef(b.PRURL); ok {
			refs = addRef(refs, seen, ref)
		}
	}
	refs = addRef(refs, seen, firstBranch(run))
	return refs
}

func addRef(refs []string, seen map[string]struct{}, ref string) []string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return refs
	}
	if _, ok := seen[ref]; ok {
		return refs
	}
	seen[ref] = struct{}{}
	return append(refs, ref)
}

func prRef(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	path := raw
	if u, err := url.Parse(raw); err == nil {
		if u.Path != "" {
			path = u.Path
		} else if u.Opaque != "" {
			path = u.Opaque
		}
	}
	m := prPath.FindStringSubmatch(path)
	if m == nil {
		return "", false
	}
	return "#" + m[1], true
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

func safeID(id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
