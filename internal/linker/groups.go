package linker

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

// softWindow is how long after a session ends a commit can still belong to it.
const softWindow = 2 * time.Hour

// groups is the mutable link graph. hard marks union-find roots that pass 1
// published as a work item. Soft links may attach a free event to one hard
// root, and they may join free events together. They do not merge two hard
// roots, and they do not merge two components that each already contain a
// session.
type groups struct {
	*hardLinker
	hard map[int]bool
}

func (g *groups) freezeHard() {
	for _, idxs := range g.components() {
		if len(idxs) < 2 {
			continue
		}
		g.hard[g.uf.find(idxs[0])] = true
	}
}

func (g *groups) components() map[int][]int {
	out := map[int][]int{}
	for i := range g.events {
		if len(g.reasons[i]) == 0 {
			continue
		}
		root := g.uf.find(i)
		out[root] = append(out[root], i)
	}
	return out
}

func (g *groups) isHard(i int) bool {
	return g.hard[g.uf.find(i)]
}

func (g *groups) sessionCount(root int) int {
	root = g.uf.find(root)
	n := 0
	for i, ev := range g.events {
		if ev.Kind == event.KindAgentSession && g.uf.find(i) == root {
			n++
		}
	}
	return n
}

// softUnion joins a and b when that does not merge two hard-link groups or
// two groups that each already contain a session. Already-joined events
// report success.
func (g *groups) softUnion(a, b int) bool {
	ra, rb := g.uf.find(a), g.uf.find(b)
	if ra == rb {
		return true
	}
	if g.hard[ra] && g.hard[rb] {
		return false
	}
	if g.sessionCount(ra) > 0 && g.sessionCount(rb) > 0 {
		return false
	}
	hard := g.hard[ra] || g.hard[rb]
	g.uf.union(a, b)
	if hard {
		g.hard[g.uf.find(a)] = true
	}
	return true
}

// attachNew records reason on both events when the soft union is new.
func (g *groups) attachNew(a, b int, reason string) bool {
	if reason == "" || a == b {
		return false
	}
	if g.uf.find(a) == g.uf.find(b) {
		return false
	}
	if !g.softUnion(a, b) {
		return false
	}
	g.addReason(a, reason)
	g.addReason(b, reason)
	return true
}

func (g *groups) repoOf(i int) string {
	if repo := g.events[i].Repo; repo != "" {
		return repo
	}
	return g.repoForCWD(g.events[i].CWD)
}

func (g *groups) repoForCWD(cwd string) string {
	if cwd == "" {
		return ""
	}
	if repo, ok := g.cwdRepo[cwd]; ok {
		return repo
	}
	cleaned := slashClean(cwd)
	if repo, ok := g.cwdRepo[cleaned]; ok {
		return repo
	}
	bestLen := -1
	best := ""
	ambiguous := false
	for root, repo := range g.cwdRepo {
		rootClean := slashClean(root)
		if rootClean == "" {
			continue
		}
		if cleaned != rootClean && !strings.HasPrefix(cleaned, rootClean+"/") {
			continue
		}
		if len(rootClean) > bestLen {
			bestLen = len(rootClean)
			best = repo
			ambiguous = false
			continue
		}
		if len(rootClean) == bestLen && repo != best {
			ambiguous = true
		}
	}
	if ambiguous {
		return ""
	}
	return best
}

func (g *groups) finish(asOf time.Time) []linkedItem {
	comps := g.components()
	if len(comps) == 0 {
		return nil
	}
	items := make([]linkedItem, 0, len(comps))
	for _, idxs := range comps {
		sortIndexes(g.events, idxs)
		members := make([]store.Record, 0, len(idxs))
		linked := make([]linkedEvent, 0, len(idxs))
		for _, idx := range idxs {
			reason := joinReasons(g.reasons[idx])
			if reason == "" {
				continue
			}
			members = append(members, g.events[idx])
			linked = append(linked, linkedEvent{Event: g.events[idx], Reason: reason})
		}
		if len(linked) == 0 {
			continue
		}
		title := chooseTitle(members)
		if title == "" {
			title = "misc"
		}
		items = append(items, linkedItem{
			Title:  title,
			Status: statusOf(members, asOf),
			Events: linked,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return eventLess(items[i].Events[0].Event, items[j].Events[0].Event)
	})
	return items
}

func sortIndexes(events []store.Record, idxs []int) {
	sort.Slice(idxs, func(a, b int) bool {
		return eventLess(events[idxs[a]], events[idxs[b]])
	})
}

func sessionEnd(ev store.Record) time.Time {
	if ev.TSEnd != nil && !ev.TSEnd.Before(ev.TS) {
		return ev.TSEnd.UTC()
	}
	return ev.TS.UTC()
}

func inSessionWindow(commit, session store.Record) bool {
	start := session.TS.UTC()
	end := sessionEnd(session).Add(softWindow)
	ts := commit.TS.UTC()
	return !ts.Before(start) && !ts.After(end)
}

func windowsOverlap(a, b store.Record) bool {
	a0, a1 := a.TS.UTC(), sessionEnd(a).Add(softWindow)
	b0, b1 := b.TS.UTC(), sessionEnd(b).Add(softWindow)
	return !a0.After(b1) && !b0.After(a1)
}

func overlapCount(left, right, cwds []string) int {
	roots := make([]string, 0, len(cwds))
	for _, cwd := range cwds {
		if root := slashClean(cwd); root != "" {
			roots = append(roots, root)
		}
	}
	rightForms := map[string]struct{}{}
	for _, path := range right {
		for form := range pathForms(path, roots) {
			rightForms[form] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	n := 0
	for _, path := range left {
		key := slashClean(path)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		if !formsMatch(pathForms(path, roots), rightForms) {
			continue
		}
		seen[key] = struct{}{}
		n++
	}
	return n
}

func formsMatch(forms, right map[string]struct{}) bool {
	for form := range forms {
		if _, ok := right[form]; ok {
			return true
		}
	}
	return false
}

// pathForms lists the path and the same path relative to each working directory.
// A relative path is also joined onto each working directory, so a session
// file recorded under a subdirectory still matches the repo-relative commit path.
func pathForms(path string, roots []string) map[string]struct{} {
	path = slashClean(path)
	out := map[string]struct{}{}
	if path == "" {
		return out
	}
	add := func(candidate string) {
		candidate = slashClean(candidate)
		if candidate == "" {
			return
		}
		out[strings.TrimPrefix(candidate, "/")] = struct{}{}
		for _, root := range roots {
			if candidate == root {
				continue
			}
			if strings.HasPrefix(candidate, root+"/") {
				out[strings.TrimPrefix(candidate, root+"/")] = struct{}{}
			}
		}
	}
	add(path)
	if !strings.HasPrefix(path, "/") {
		for _, root := range roots {
			add(root + "/" + path)
		}
	}
	return out
}

func slashClean(path string) string {
	path = strings.TrimSpace(path)
	path = filepath.ToSlash(path)
	for strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	path = strings.TrimSuffix(path, "/")
	if path == "" || path == "." {
		return ""
	}
	return path
}
