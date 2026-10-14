package linker

import (
	"regexp"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/store"
)

// linkEvents runs hard links, soft links, leftovers, and status rules.
// asOf is the clock for the stale-branch rule. loc is the timezone used to
// choose the local day of a leftover. A nil loc uses UTC.
func linkEvents(events []store.Record, patterns []*regexp.Regexp, deny bool, asOf time.Time, loc *time.Location) []linkedItem {
	return linkEventsWith(events, patterns, deny, asOf, loc, nil)
}

// linkEventsWith is linkEvents followed by user overrides.
// Overrides are applied after pass 3, in the order given.
func linkEventsWith(events []store.Record, patterns []*regexp.Regexp, deny bool, asOf time.Time, loc *time.Location, overrides []store.Override) []linkedItem {
	if len(events) == 0 {
		return nil
	}
	if loc == nil {
		loc = time.UTC
	}
	g := &groups{
		hardLinker: newHardLinker(events, patterns, deny),
		hard:       map[int]bool{},
	}
	g.freezeHard()
	g.linkSessionCommits()
	g.linkGitBranches()
	g.linkReverts()
	g.pass3(loc)
	g.applyOverrides(overrides)
	return g.finish(asOf)
}
