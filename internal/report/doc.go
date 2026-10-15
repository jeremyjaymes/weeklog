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
// Markdown comes from an embedded text/template. output.template replaces it.
// Text between <!-- weeklog:notes --> markers is kept when a Markdown file
// is overwritten. JSON is the same report without the notes block.
//
// The terminal recap is a separate view of the same report. It is a bordered
// summary: one bar per day, a lane per source, and the top repositories and
// pull requests. Recap.Delta and Recap.Graph are empty slots. A later change
// can fill Delta with a week-over-week line, or Graph with the session to
// commit to pull-request chain, without changing the renderer.
package report
