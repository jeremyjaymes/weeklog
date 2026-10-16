# Linking

weeklog groups stored events into work items. A work item has a title, a status, and one stored reason for each event in it. Linking is deterministic and stores a reason on every link. It does not call a language model.

`weeklog collect` runs the linker after sources finish. `weeklog link-run` runs it again without collecting. Both commands read every stored event, not only the events inside the time window. The window still matters: its end is the clock for a stale branch, and its timezone chooses the local day for leftovers.

```bash
weeklog link-run
weeklog link-run --week 2026-W41
```

## Pass 1, hard links

Pass 1 builds a graph with union-find. A link is made only when one of these rules matches:

- A commit SHA listed on a pull request joins that commit and the pull request.
- A ticket key joins the events that mention it. The key is matched in a branch name, a commit message, a pull request title or body, a session prompt, the event refs, or the ticket's own id.
- A pull request's head branch joins that pull request to commits and sessions on the same branch in the same repository.
- A pull request joins its reviews when their source ids share a lineage, such as `acme/otto#12` and `acme/otto#12#review-9`.

Ticket keys come from each source's `ticket_pattern`. When none is set, weeklog uses a built-in key shape such as `GEN-301`. Names that only look like keys (`ISO-8601`, `RFC-123`, `SHA-1`, and the same family) are not treated as tickets unless a source sets `ticket_pattern`.

## Pass 2, soft links

Pass 2 only attaches an event that pass 1 left out of a hard-link group. It does not merge two hard-link groups, and it does not merge two groups that each already contain an agent session.

- An agent session whose working directory resolves to a repository joins commits in that repository authored between the session start and two hours after the session end, when they share at least one file.
- With no shared file, that session joins the commits in the window only when it is the only session whose window overlaps.
- A session joins a branch when its text records a `git checkout` or `git switch`, when its own branch is set, or when the commits in the window share one HEAD branch.
- A revert commit joins the commit it reverts.

## Pass 3, leftovers

Events that are still alone are grouped by repository and local day. The day is chosen in the window's timezone. A session with no commits is its own item. A session that is still alone becomes its own item.

## Status and title

Status is chosen after the groups are final, including after overrides. The first matching rule wins:

1. `abandoned` when the item contains a revert.
2. `shipped` when it has a merged pull request, a closed or done ticket, or a commit on `main`, `master`, or `trunk`.
3. `abandoned` when it is a side branch with no pull request and no activity for 7 days.
4. `in progress` when it has an open pull request or an open ticket.
5. `explored` when it has a closed pull request that was not merged.
6. `in progress` when it has a commit on any other branch.
7. `explored` when it contains only agent sessions.

An item that matches none of these has an empty status.

The title is the first ticket title, then the first pull request title, then the most common commit subject, then the first session prompt.

## Link reasons

Every event in a work item stores why it is there. When several rules match, the reasons are joined with `, ` in a stable order. `weeklog explain` prints that stored text. `weeklog week` lists the same reasons on the item as `Linked by:`.

| Reason | Meaning |
|---|---|
| `ref:GEN-301 ticket` | The event is the ticket whose key is `GEN-301` |
| `ref:GEN-301 transition` | The event is a transition for that ticket |
| `ref:GEN-301 in branch` | The key appears in the branch name |
| `ref:GEN-301 in commit message` | The key appears in the commit title or body |
| `ref:GEN-301 in title` | The key appears in a pull request title |
| `ref:GEN-301 in body` | The key appears in a pull request body |
| `ref:GEN-301 in prompt` | The key appears in a session title or prompt |
| `ref:GEN-301 in refs` | The key appears in the event's refs |
| `sha:<sha> in pr` | The pull request lists that commit SHA |
| `branch:<name>` | A pull request head branch, or a session's own branch, matches `<name>` |
| `lineage:<source-id>` | A review shares a source id prefix with its pull request |
| `files overlap: 3` | A session and a commit share 3 files inside the session window |
| `head branch:<name>` | The only session in the window shares the commits' HEAD branch |
| `only session in window` | The session is the only one overlapping those commits, and they share no file |
| `git branch:<name>` | The session text records a checkout or switch to `<name>` |
| `revert:<sha>` | The commit reverts `<sha>` |
| `session only` | Pass 3 left the session on its own |
| `leftover:<repo> <YYYY-MM-DD>` | Pass 3 grouped the event with other leftovers for that repository and local day. `<repo>` is `-` when the event has no repository |
| `override:link` | `weeklog link` joined this event with another, and that join still holds |
| `override:unlink` | `weeklog unlink` detached this event, and a later link has not put it back |

## Overrides

`weeklog link` and `weeklog unlink` append a row to the `overrides` table and then run the linker. The table is added by a versioned migration when the database is opened. The migration runs in one transaction, the same way earlier schema versions do.

Each row stores the kind (`link` or `unlink`), the event row ids, and the time it was recorded. It does not copy event text. Event text is redacted when the event is stored, and the override does not keep a second copy. Deleting an event removes the overrides that name it.

The linker applies those rows after pass 3, including on the next `link-run` or `collect`. It replays the rows oldest first. That is later than the hard-link and soft-link rules, so an override can join two hard-link groups or pull one event out of a hard link. Pass 2 will not do either of those. A later row wins: unlink, then link, leaves the events together; link, then unlink, leaves the named event on its own.

The correction is still there after `weeklog link-run` and after `weeklog collect`. Collect upserts events on `(source, source_id)` and keeps the same row id, so the override still points at the event.

```bash
weeklog link linear/GEN-301 git/0123456789abcdef0123456789abcdef01234567
weeklog link event:1 event:2
weeklog unlink git/0123456789abcdef0123456789abcdef01234567
weeklog unlink event:4
```

An event is `SOURCE/SOURCE_ID` or `event:ID`. A bare number is the event row id. `SOURCE/SOURCE_ID` splits on the first slash, so a source id may contain slashes (`github/acme/otto#12`). The source and source id are the stored values and are case-sensitive.

`weeklog link` and `weeklog unlink` take the same window flags as `weeklog link-run` (`--week`, `--since`, `--until`). The window chooses the stale-branch clock and the local day for the linker run those commands start. `--db` selects the database.

`weeklog explain` prints the group and each stored reason:

```bash
weeklog explain 1
weeklog explain item:1
weeklog explain event:4
weeklog explain linear/GEN-301
```

A bare number or `item:ID` is a work item id. Those ids are rewritten from 1 each time the linker runs, in the order of the groups. `SOURCE/SOURCE_ID` and `event:ID` keep pointing at the same event. An event that is not in a work item is printed as `not linked`.

```text
work item 1
title: Webhook retries
status: shipped

linear/GEN-301
  id: 1
  kind: ticket
  ts: 2026-10-06T12:00:00Z
  title: Webhook retries
  reason: ref:GEN-301 ticket, override:link

git/0123456789abcdef0123456789abcdef01234567
  id: 3
  kind: commit
  ts: 2026-10-09T15:00:00Z
  title: unrelated notes
  reason: leftover:github.com/jeremyjaymes/otto 2026-10-09, override:link
```

`weeklog week` shows the same reasons, including `override:link` and `override:unlink`, in the item's `Linked by:` line, for both Markdown and JSON. The terminal recap also draws them on the session chain, dimmed under each session, commit, pull request, or issue. Soft-link reasons (`files overlap`, `git branch`, `head branch`, `only session in window`, `revert`) use `o` and `:`. Hard-link reasons use `*` and `|`. `--no-graph` hides that panel. A pipe, `NO_COLOR`, or `--plain` still prints the Markdown report. `weeklog week -i` opens the same week as a scrollable day list and drills from a day to an item to those linker-attached commits, pull requests, and sessions.
