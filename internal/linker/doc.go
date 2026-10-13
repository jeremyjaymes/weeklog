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
// Run replaces the work items in the store with those groups. Soft links,
// leftover grouping, and status rules are later passes. There is no LLM.
package linker
