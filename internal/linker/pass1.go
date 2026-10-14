package linker

import (
	"regexp"
	"sort"
	"strings"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

// shaRef matches one commit SHA stored in a pull request's refs.
// The GitHub source puts the pull request's commit list there.
var shaRef = regexp.MustCompile(`(?i)^[0-9a-f]{7,40}$`)

// lineageSeps separate a parent source_id from a child, as in
// acme/otto#12 and acme/otto#12#review-9.
const lineageSeps = "#/:-"

type linkedEvent struct {
	Event  store.Record
	Reason string
}

type linkedItem struct {
	Title  string
	Status string
	Events []linkedEvent
}

type branchKey struct {
	repo   string
	branch string
}

type commitSHA struct {
	idx int
	sha string
}

type keyHit struct {
	key   string
	place string
}

// pass1 groups events into hard-link work items.
// patterns and deny come from compilePatterns. Events that share no hard
// link are omitted; later passes group those.
func pass1(events []store.Record, patterns []*regexp.Regexp, deny bool) []linkedItem {
	if len(events) == 0 {
		return nil
	}
	return newHardLinker(events, patterns, deny).items()
}

func newHardLinker(events []store.Record, patterns []*regexp.Regexp, deny bool) *hardLinker {
	l := &hardLinker{
		events:  events,
		uf:      newUF(len(events)),
		reasons: make([]map[string]struct{}, len(events)),
		tickets: map[string]int{},
		match:   ticketMatcher{res: patterns, deny: deny},
		cwdRepo: reposByCWD(events),
	}
	l.linkTickets()
	l.linkSHAs()
	l.linkBranches()
	l.linkLineage()
	return l
}

type hardLinker struct {
	events  []store.Record
	uf      *uf
	reasons []map[string]struct{}
	tickets map[string]int
	match   ticketMatcher
	cwdRepo map[string]string
}

func (l *hardLinker) addReason(idx int, reason string) {
	if reason == "" {
		return
	}
	if l.reasons[idx] == nil {
		l.reasons[idx] = map[string]struct{}{}
	}
	l.reasons[idx][reason] = struct{}{}
}

func (l *hardLinker) ticketNode(key string) int {
	if n, ok := l.tickets[key]; ok {
		return n
	}
	n := l.uf.add()
	l.tickets[key] = n
	return n
}

func (l *hardLinker) linkTickets() {
	for i, ev := range l.events {
		for _, hit := range l.ticketHits(ev) {
			l.uf.union(i, l.ticketNode(hit.key))
			l.addReason(i, "ref:"+hit.key+" "+hit.place)
		}
	}
}

func (l *hardLinker) ticketHits(ev store.Record) []keyHit {
	var hits []keyHit
	seen := map[string]struct{}{}
	add := func(key, place string) {
		if key == "" {
			return
		}
		id := key + "\x00" + place
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		hits = append(hits, keyHit{key: key, place: place})
	}

	identity := ""
	if ev.Kind == event.KindTicket || ev.Kind == event.KindTicketTransition {
		identity = l.identityKey(ev)
		if identity != "" {
			place := "ticket"
			if ev.Kind == event.KindTicketTransition {
				place = "transition"
			}
			add(identity, place)
		}
	}

	for _, key := range l.match.keys(ev.Branch) {
		if key != identity {
			add(key, "in branch")
		}
	}
	switch ev.Kind {
	case event.KindCommit:
		seenKey := map[string]struct{}{}
		for _, text := range []string{ev.Title, ev.Summary} {
			for _, key := range l.match.keys(text) {
				if key == identity {
					continue
				}
				if _, ok := seenKey[key]; ok {
					continue
				}
				seenKey[key] = struct{}{}
				add(key, "in commit message")
			}
		}
	case event.KindPR:
		for _, key := range l.match.keys(ev.Title) {
			if key != identity {
				add(key, "in title")
			}
		}
		for _, key := range l.match.keys(ev.Summary) {
			if key != identity {
				add(key, "in body")
			}
		}
	case event.KindAgentSession:
		seenKey := map[string]struct{}{}
		for _, text := range []string{ev.Summary, ev.Title} {
			for _, key := range l.match.keys(text) {
				if key == identity {
					continue
				}
				if _, ok := seenKey[key]; ok {
					continue
				}
				seenKey[key] = struct{}{}
				add(key, "in prompt")
			}
		}
	}

	found := map[string]struct{}{}
	for _, hit := range hits {
		found[hit.key] = struct{}{}
	}
	for _, ref := range ev.Refs {
		for _, key := range l.match.keys(ref) {
			if key == identity {
				continue
			}
			if _, ok := found[key]; ok {
				continue
			}
			add(key, "in refs")
			found[key] = struct{}{}
		}
	}
	return hits
}

func (l *hardLinker) identityKey(ev store.Record) string {
	if key, ok := l.wholeKey(ev.SourceID); ok {
		return key
	}
	for _, ref := range ev.Refs {
		if key, ok := l.wholeKey(ref); ok {
			return key
		}
	}
	if keys := l.match.keys(ev.Title); len(keys) > 0 {
		return keys[0]
	}
	if keys := l.match.keys(ev.Summary); len(keys) > 0 {
		return keys[0]
	}
	return ""
}

func (l *hardLinker) wholeKey(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	keys := l.match.keys(value)
	if len(keys) == 1 && strings.EqualFold(keys[0], value) {
		return keys[0], true
	}
	return "", false
}

func (l *hardLinker) linkSHAs() {
	var commits []commitSHA
	for i, ev := range l.events {
		if ev.Kind != event.KindCommit {
			continue
		}
		sha := strings.ToLower(strings.TrimSpace(ev.SourceID))
		if shaRef.MatchString(sha) {
			commits = append(commits, commitSHA{idx: i, sha: sha})
		}
	}
	if len(commits) == 0 {
		return
	}
	for i, ev := range l.events {
		if ev.Kind != event.KindPR {
			continue
		}
		best := map[int]string{}
		for _, ref := range ev.Refs {
			ref = strings.ToLower(strings.TrimSpace(ref))
			if !shaRef.MatchString(ref) {
				continue
			}
			idx, ok := matchCommit(ref, commits)
			if !ok || idx == i {
				continue
			}
			if prev, exists := best[idx]; !exists || len(ref) > len(prev) {
				best[idx] = ref
			}
		}
		idxs := make([]int, 0, len(best))
		for idx := range best {
			idxs = append(idxs, idx)
		}
		sort.Ints(idxs)
		for _, idx := range idxs {
			reason := "sha:" + best[idx] + " in pr"
			l.uf.union(i, idx)
			l.addReason(i, reason)
			l.addReason(idx, reason)
		}
	}
}

func matchCommit(ref string, commits []commitSHA) (int, bool) {
	exact := -1
	var prefix []int
	for _, commit := range commits {
		switch {
		case commit.sha == ref:
			exact = commit.idx
		case strings.HasPrefix(commit.sha, ref):
			prefix = append(prefix, commit.idx)
		}
	}
	if exact >= 0 {
		return exact, true
	}
	if len(prefix) == 1 {
		return prefix[0], true
	}
	return 0, false
}

func (l *hardLinker) linkBranches() {
	prs := map[branchKey][]int{}
	for i, ev := range l.events {
		if ev.Kind != event.KindPR {
			continue
		}
		key, ok := l.branchOf(ev)
		if !ok {
			continue
		}
		prs[key] = append(prs[key], i)
	}
	if len(prs) == 0 {
		return
	}
	for i, ev := range l.events {
		if ev.Kind != event.KindCommit && ev.Kind != event.KindAgentSession {
			continue
		}
		key, ok := l.branchOf(ev)
		if !ok {
			continue
		}
		for _, pr := range prs[key] {
			reason := "branch:" + key.branch
			l.uf.union(i, pr)
			l.addReason(i, reason)
			l.addReason(pr, reason)
		}
	}
}

func (l *hardLinker) branchOf(ev store.Record) (branchKey, bool) {
	repo := ev.Repo
	if repo == "" {
		repo = l.cwdRepo[ev.CWD]
	}
	branch := normBranch(ev.Branch)
	if repo == "" || branch == "" {
		return branchKey{}, false
	}
	return branchKey{repo: repo, branch: branch}, true
}

func (l *hardLinker) linkLineage() {
	bySource := map[string][]int{}
	for i, ev := range l.events {
		if ev.Source == "" || ev.SourceID == "" {
			continue
		}
		bySource[ev.Source] = append(bySource[ev.Source], i)
	}
	for _, idxs := range bySource {
		for _, child := range idxs {
			parent := -1
			parentID := ""
			childID := l.events[child].SourceID
			for _, cand := range idxs {
				if cand == child {
					continue
				}
				id := l.events[cand].SourceID
				if isLineageParent(id, childID) && len(id) > len(parentID) {
					parent = cand
					parentID = id
				}
			}
			if parent < 0 {
				continue
			}
			reason := "lineage:" + parentID
			l.uf.union(child, parent)
			l.addReason(child, reason)
			l.addReason(parent, reason)
		}
	}
}

func (l *hardLinker) items() []linkedItem {
	groups := map[int][]int{}
	for i := range l.events {
		if len(l.reasons[i]) == 0 {
			continue
		}
		root := l.uf.find(i)
		groups[root] = append(groups[root], i)
	}
	if len(groups) == 0 {
		return nil
	}
	items := make([]linkedItem, 0, len(groups))
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		sort.Slice(idxs, func(a, b int) bool {
			return eventLess(l.events[idxs[a]], l.events[idxs[b]])
		})
		members := make([]store.Record, 0, len(idxs))
		linked := make([]linkedEvent, 0, len(idxs))
		for _, idx := range idxs {
			reason := joinReasons(l.reasons[idx])
			if reason == "" {
				continue
			}
			members = append(members, l.events[idx])
			linked = append(linked, linkedEvent{Event: l.events[idx], Reason: reason})
		}
		if len(linked) < 2 {
			continue
		}
		items = append(items, linkedItem{Title: chooseTitle(members), Events: linked})
	}
	sort.Slice(items, func(i, j int) bool {
		return eventLess(items[i].Events[0].Event, items[j].Events[0].Event)
	})
	return items
}

func joinReasons(set map[string]struct{}) string {
	if len(set) == 0 {
		return ""
	}
	reasons := make([]string, 0, len(set))
	for reason := range set {
		reasons = append(reasons, reason)
	}
	sort.Slice(reasons, func(i, j int) bool {
		ri, rj := reasonRank(reasons[i]), reasonRank(reasons[j])
		if ri != rj {
			return ri < rj
		}
		return reasons[i] < reasons[j]
	})
	return strings.Join(reasons, ", ")
}

func reasonRank(reason string) int {
	switch {
	case strings.HasPrefix(reason, "ref:"):
		return 0
	case strings.HasPrefix(reason, "sha:"):
		return 1
	case strings.HasPrefix(reason, "branch:"):
		return 2
	case strings.HasPrefix(reason, "lineage:"):
		return 3
	case strings.HasPrefix(reason, "files overlap:"):
		return 4
	case strings.HasPrefix(reason, "git branch:"), strings.HasPrefix(reason, "head branch:"):
		return 5
	case strings.HasPrefix(reason, "only session"):
		return 6
	case strings.HasPrefix(reason, "revert:"):
		return 7
	case strings.HasPrefix(reason, "leftover:"), reason == "session only":
		return 8
	default:
		return 9
	}
}

func chooseTitle(events []store.Record) string {
	ordered := append([]store.Record(nil), events...)
	sort.Slice(ordered, func(i, j int) bool { return eventLess(ordered[i], ordered[j]) })
	if title := firstTitle(ordered, event.KindTicket); title != "" {
		return title
	}
	if title := firstTitle(ordered, event.KindPR); title != "" {
		return title
	}
	if title := commonCommitTitle(ordered); title != "" {
		return title
	}
	for _, ev := range ordered {
		if ev.Kind != event.KindAgentSession {
			continue
		}
		if title := strings.TrimSpace(ev.Title); title != "" {
			return title
		}
		if line := firstLine(ev.Summary); line != "" {
			return line
		}
	}
	return ""
}

func firstTitle(events []store.Record, kind event.Kind) string {
	for _, ev := range events {
		if ev.Kind != kind {
			continue
		}
		if title := strings.TrimSpace(ev.Title); title != "" {
			return title
		}
	}
	return ""
}

func commonCommitTitle(events []store.Record) string {
	type stat struct {
		n     int
		first store.Record
	}
	stats := map[string]*stat{}
	var best string
	var bestStat *stat
	for _, ev := range events {
		if ev.Kind != event.KindCommit {
			continue
		}
		title := strings.TrimSpace(ev.Title)
		if title == "" {
			continue
		}
		st := stats[title]
		if st == nil {
			st = &stat{first: ev}
			stats[title] = st
		}
		st.n++
		if bestStat == nil || st.n > bestStat.n || (st.n == bestStat.n && eventLess(st.first, bestStat.first)) {
			best = title
			bestStat = st
		}
	}
	return best
}

func eventLess(a, b store.Record) bool {
	if !a.TS.Equal(b.TS) {
		return a.TS.Before(b.TS)
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	if a.SourceID != b.SourceID {
		return a.SourceID < b.SourceID
	}
	return a.ID < b.ID
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func normBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	branch = strings.TrimPrefix(branch, "refs/heads/")
	if rest, ok := strings.CutPrefix(branch, "refs/remotes/"); ok {
		_, name, found := strings.Cut(rest, "/")
		if !found {
			return ""
		}
		return name
	}
	return branch
}

func reposByCWD(events []store.Record) map[string]string {
	sets := map[string]map[string]struct{}{}
	for _, ev := range events {
		if ev.Repo == "" || ev.CWD == "" {
			continue
		}
		if sets[ev.CWD] == nil {
			sets[ev.CWD] = map[string]struct{}{}
		}
		sets[ev.CWD][ev.Repo] = struct{}{}
	}
	out := make(map[string]string, len(sets))
	for cwd, repos := range sets {
		if len(repos) != 1 {
			continue
		}
		for repo := range repos {
			out[cwd] = repo
		}
	}
	return out
}

func isLineageParent(parent, child string) bool {
	if parent == "" || len(parent) >= len(child) {
		return false
	}
	if !strings.HasPrefix(child, parent) {
		return false
	}
	return strings.ContainsRune(lineageSeps, rune(child[len(parent)]))
}

type uf struct {
	parent []int
}

func newUF(n int) *uf {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	return &uf{parent: parent}
}

func (u *uf) add() int {
	u.parent = append(u.parent, len(u.parent))
	return len(u.parent) - 1
}

func (u *uf) find(x int) int {
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x])
	}
	return u.parent[x]
}

func (u *uf) union(a, b int) {
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
