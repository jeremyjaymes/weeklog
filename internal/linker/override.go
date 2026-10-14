package linker

import "github.com/jeremyjaymes/weeklog/internal/store"

const (
	reasonOverrideLink   = "override:link"
	reasonOverrideUnlink = "override:unlink"
)

// applyOverrides replays user corrections after pass 3.
// A link joins the two events' groups, including when both groups were hard
// links. An unlink detaches that event. Later rows win. An override that
// names a missing event is skipped. Reasons are taken from the automatic
// passes, then adjusted for the final membership: a link that still holds
// adds override:link on the two named events, and an event whose latest
// correction detached it stores only override:unlink.
func (g *groups) applyOverrides(overrides []store.Override) {
	if len(overrides) == 0 || len(g.events) == 0 {
		return
	}
	indexOf := make(map[int64]int, len(g.events))
	for i, ev := range g.events {
		if ev.ID == 0 {
			continue
		}
		indexOf[ev.ID] = i
	}
	original := make([]map[string]struct{}, len(g.events))
	for i, reasons := range g.reasons {
		original[i] = cloneReasons(reasons)
	}

	sets := newIndexUF(len(g.events))
	rooted := map[int]int{}
	for i := range g.events {
		root := g.uf.find(i)
		if prev, ok := rooted[root]; ok {
			sets.union(prev, i)
			continue
		}
		rooted[root] = i
	}

	// lastUnlink is true when the latest override that named the event detached it.
	lastUnlink := map[int]bool{}
	for _, ov := range overrides {
		switch ov.Kind {
		case store.OverrideLink:
			a, okA := indexOf[ov.EventA]
			b, okB := indexOf[ov.EventB]
			if !okA || !okB || a == b {
				continue
			}
			sets.union(a, b)
			lastUnlink[a] = false
			lastUnlink[b] = false
		case store.OverrideUnlink:
			a, ok := indexOf[ov.EventA]
			if !ok {
				continue
			}
			sets.detach(a)
			lastUnlink[a] = true
		}
	}

	for i := range g.events {
		if lastUnlink[i] {
			g.reasons[i] = map[string]struct{}{reasonOverrideUnlink: {}}
			continue
		}
		g.reasons[i] = cloneReasons(original[i])
	}
	for _, ov := range overrides {
		if ov.Kind != store.OverrideLink {
			continue
		}
		a, okA := indexOf[ov.EventA]
		b, okB := indexOf[ov.EventB]
		if !okA || !okB || a == b || lastUnlink[a] || lastUnlink[b] {
			continue
		}
		if sets.find(a) != sets.find(b) {
			continue
		}
		g.addReason(a, reasonOverrideLink)
		g.addReason(b, reasonOverrideLink)
	}

	g.uf = newUF(len(g.events))
	seen := map[int]int{}
	for i := range g.events {
		root := sets.find(i)
		if prev, ok := seen[root]; ok {
			g.uf.union(prev, i)
			continue
		}
		seen[root] = i
	}
}

func cloneReasons(src map[string]struct{}) map[string]struct{} {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]struct{}, len(src))
	for reason := range src {
		dst[reason] = struct{}{}
	}
	return dst
}

// indexUF is a union-find over event indexes that can detach one index.
// The pass-1 union-find also holds ticket nodes, so overrides use this copy.
type indexUF struct {
	parent []int
}

func newIndexUF(n int) *indexUF {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	return &indexUF{parent: parent}
}

func (u *indexUF) find(x int) int {
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x])
	}
	return u.parent[x]
}

func (u *indexUF) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if ra < rb {
		u.parent[rb] = ra
		return
	}
	u.parent[ra] = rb
}

// detach pulls target out of its component and leaves the others joined.
func (u *indexUF) detach(target int) {
	root := u.find(target)
	members := make([]int, 0)
	for i := range u.parent {
		if u.find(i) == root {
			members = append(members, i)
		}
	}
	for _, i := range members {
		u.parent[i] = i
	}
	first := -1
	for _, i := range members {
		if i == target {
			continue
		}
		if first < 0 {
			first = i
			continue
		}
		u.union(first, i)
	}
}
