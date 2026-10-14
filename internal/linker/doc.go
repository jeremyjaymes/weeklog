// Package linker groups stored events into units of work.
//
// Pass 1 builds hard links with union-find. A link is made only when one of
// these rules matches, and the reason is stored with the event:
//
//   - a commit SHA listed on a pull request joins that commit and the pull request
//   - a ticket key joins the events that mention it to the ticket
//   - a pull request's head branch joins that pull request to commits and
//     sessions on the same branch in the same repository
//   - a shared source_id lineage joins a pull request and its reviews
//
// Pass 2 adds soft links. It only attaches an event that pass 1 left out of
// a hard-link group, and it does not merge two hard-link groups:
//
//   - an agent session whose working directory resolves to a repository joins
//     commits there that were authored between the session start and two hours
//     after the session end, when they share a file
//   - with no shared file, that session joins the commits in the window only
//     when it is the only session whose window overlaps
//   - a session joins a branch when its text records a git checkout or switch,
//     when its own branch is set, or when the commits in the window share one
//     HEAD branch
//   - a revert commit joins the commit it reverts
//
// Pass 3 groups events that are still alone by repository and local day. A
// session that is still alone becomes its own item. Every link stores a
// reason. Status is shipped, in progress, explored, or abandoned.
//
// Run replaces the work items in the store. There is no LLM. Manual link
// overrides are a later pass.
package linker
