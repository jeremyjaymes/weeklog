package linker

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

type claim struct {
	session int
	commit  int
	overlap int
	dist    time.Duration
	reason  string
}

// linkSessionCommits joins a free agent session to commits in its repository.
// File overlap wins over a shared HEAD branch, which wins over the
// only-session fallback. A commit already claimed by another session stays
// there. A session may still attach to one hard-link group.
func (g *groups) linkSessionCommits() {
	var claims []claim
	for s, ev := range g.events {
		if ev.Kind != event.KindAgentSession || g.isHard(s) {
			continue
		}
		repo := g.repoOf(s)
		if repo == "" {
			continue
		}
		only := g.onlySession(s, repo)
		head := ""
		if only {
			head = g.uniqueBranch(s, repo)
		}
		for c, commit := range g.events {
			if commit.Kind != event.KindCommit || g.repoOf(c) != repo {
				continue
			}
			if !inSessionWindow(commit, ev) {
				continue
			}
			overlap := overlapCount(ev.Files, commit.Files, []string{ev.CWD, commit.CWD})
			reason, ok := softCommitReason(overlap, head, commit.Branch, only)
			if !ok {
				continue
			}
			claims = append(claims, claim{
				session: s,
				commit:  c,
				overlap: overlap,
				dist:    absDuration(commit.TS.Sub(ev.TS)),
				reason:  reason,
			})
		}
	}
	sort.Slice(claims, func(i, j int) bool {
		a, b := claims[i], claims[j]
		if a.overlap != b.overlap {
			return a.overlap > b.overlap
		}
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		if eventLess(g.events[a.session], g.events[b.session]) {
			return true
		}
		if eventLess(g.events[b.session], g.events[a.session]) {
			return false
		}
		return eventLess(g.events[a.commit], g.events[b.commit])
	})
	for _, c := range claims {
		g.attachNew(c.session, c.commit, c.reason)
	}
}

func softCommitReason(overlap int, head, commitBranch string, only bool) (string, bool) {
	if overlap > 0 {
		return fmt.Sprintf("files overlap: %d", overlap), true
	}
	if head != "" && normBranch(commitBranch) == head {
		return "head branch:" + head, true
	}
	if only {
		return "only session in window", true
	}
	return "", false
}

func (g *groups) onlySession(s int, repo string) bool {
	for i, ev := range g.events {
		if i == s || ev.Kind != event.KindAgentSession {
			continue
		}
		if g.repoOf(i) != repo {
			continue
		}
		if windowsOverlap(g.events[s], ev) {
			return false
		}
	}
	return true
}

// uniqueBranch is the branch shared by every branched commit in the session
// window. Mixed branches and a window with no branch return "".
func (g *groups) uniqueBranch(session int, repo string) string {
	branch := ""
	for i, ev := range g.events {
		if ev.Kind != event.KindCommit || g.repoOf(i) != repo {
			continue
		}
		if !inSessionWindow(ev, g.events[session]) {
			continue
		}
		b := normBranch(ev.Branch)
		if b == "" {
			continue
		}
		if branch == "" {
			branch = b
			continue
		}
		if branch != b {
			return ""
		}
	}
	return branch
}

// linkGitBranches joins a free session to a branch named by a git checkout or
// switch, or by the session's own branch field. Pull requests on that branch
// join even when they were opened after the session. Commits join only inside
// the session window. Earlier sessions claim a free commit before later ones.
func (g *groups) linkGitBranches() {
	var sessions []int
	for s, ev := range g.events {
		if ev.Kind == event.KindAgentSession && !g.isHard(s) {
			sessions = append(sessions, s)
		}
	}
	sortIndexes(g.events, sessions)
	for _, s := range sessions {
		ev := g.events[s]
		repo := g.repoOf(s)
		branch, reason := inferredGitBranch(ev)
		if repo == "" || branch == "" {
			continue
		}
		for i, other := range g.events {
			if other.Kind != event.KindPR || g.repoOf(i) != repo || normBranch(other.Branch) != branch {
				continue
			}
			g.attachNew(s, i, reason)
		}
		for i, other := range g.events {
			if other.Kind != event.KindCommit || g.repoOf(i) != repo || normBranch(other.Branch) != branch {
				continue
			}
			if !inSessionWindow(other, ev) {
				continue
			}
			g.attachNew(s, i, reason)
		}
	}
}

func inferredGitBranch(ev store.Record) (string, string) {
	if branch := lastGitBranch(ev.Title + "\n" + ev.Summary); branch != "" {
		return branch, "git branch:" + branch
	}
	if branch := normBranch(ev.Branch); branch != "" {
		return branch, "branch:" + branch
	}
	return "", ""
}

// linkReverts joins a revert commit to the commit it names. This may merge
// two session groups, because the later commit undoes the earlier one. It
// does not merge two hard-link groups.
func (g *groups) linkReverts() {
	var commits []commitSHA
	for i, ev := range g.events {
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
	for i, ev := range g.events {
		if ev.Kind != event.KindCommit || !isRevertCommit(ev) {
			continue
		}
		best := map[int]string{}
		for _, sha := range revertSHAs(ev) {
			idx, ok := matchCommit(sha, commits)
			if !ok || idx == i {
				continue
			}
			if prev, exists := best[idx]; !exists || len(sha) > len(prev) {
				best[idx] = sha
			}
		}
		idxs := make([]int, 0, len(best))
		for idx := range best {
			idxs = append(idxs, idx)
		}
		sort.Ints(idxs)
		for _, idx := range idxs {
			g.attachRevert(i, idx, "revert:"+best[idx])
		}
	}
}

func (g *groups) attachRevert(a, b int, reason string) {
	ra, rb := g.uf.find(a), g.uf.find(b)
	if ra != rb {
		if g.hard[ra] && g.hard[rb] {
			return
		}
		hard := g.hard[ra] || g.hard[rb]
		g.uf.union(a, b)
		if hard {
			g.hard[g.uf.find(a)] = true
		}
	}
	g.addReason(a, reason)
	g.addReason(b, reason)
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
