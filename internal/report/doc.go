// Package report renders a weekly log from stored events and work items.
//
// A work item is included when one of its commits, sessions, pull requests,
// or tickets starts in the window. Sessions are counted on their start time,
// including when they run past midnight. Events that are not in a work item
// are still rendered: each session becomes an Explored item, and other
// leftovers from the same repository and local day are grouped together.
// Reviews, comments, issues, ticket updates, and unknown kinds are counted
// under Other activity.
//
// Status is the stored work-item status when it is shipped, in progress,
// explored, or abandoned. Otherwise it is derived from the events. A stale
// branch is judged from the end of the window, so rendering the same week
// again does not change sections. Time is the sum of agent-session durations.
// An item with no session time uses a 15-minute estimate for each cluster of
// commits separated by more than two hours.
//
// Each item has an actor taken from stored evidence: a Co-authored-by
// trailer, a session linked to a commit, an agent branch prefix, or the
// session source when the item has no commit. A commit or pull request with
// no agent evidence is Human. Missing or conflicting evidence is Unknown.
// The summary counts items per actor. See docs/ATTRIBUTION.md.
//
// Markdown comes from an embedded text/template. output.template replaces it.
// Text between <!-- weeklog:notes --> markers is kept when a Markdown file
// is overwritten. JSON is the same report without the notes block.
//
// The terminal recap is a separate view of the same report. It is a bordered
// summary: one bar per day, a lane per source, per-actor counts, the session
// chain, and the top repositories and pull requests. The chain is built from
// linker work items inside the window: an AI session, the commits it produced,
// and the pull request or issue it was linked to. The stored reason is dimmed. A
// soft link is drawn with o and :, and a hard link with * and |. Long chains
// collapse to a "+N more" row. The same panel is drawn when color is off.
// Recap.Delta is an empty slot for a later week-over-week line.
package report
