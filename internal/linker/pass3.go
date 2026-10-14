package linker

import (
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// pass3 groups events that are still outside a multi-event work item.
// Sessions become their own items. Everything else is grouped by repository
// and local day. Events that already carry a hard-link reason keep it.
func (g *groups) pass3(loc *time.Location) {
	assigned := map[int]bool{}
	for _, idxs := range g.components() {
		if len(idxs) < 2 {
			continue
		}
		for _, i := range idxs {
			assigned[i] = true
		}
	}
	type key struct {
		repo string
		day  string
	}
	buckets := map[key][]int{}
	var order []key
	for i, ev := range g.events {
		if assigned[i] {
			continue
		}
		if ev.Kind == event.KindAgentSession {
			g.addReason(i, "session only")
			continue
		}
		k := key{repo: g.repoOf(i), day: ev.TS.In(loc).Format("2006-01-02")}
		if _, ok := buckets[k]; !ok {
			order = append(order, k)
		}
		buckets[k] = append(buckets[k], i)
	}
	for _, k := range order {
		idxs := buckets[k]
		label := k.repo
		if label == "" {
			label = "-"
		}
		reason := "leftover:" + label + " " + k.day
		for _, i := range idxs {
			g.addReason(i, reason)
		}
		for _, i := range idxs[1:] {
			g.uf.union(idxs[0], i)
		}
	}
}
