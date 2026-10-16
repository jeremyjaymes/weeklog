# Attribution

weeklog tags each work item with one actor. The tag is evidence from the stored events. The rules below run in order. The first rule that finds evidence wins. A rule that finds two actors, or an agent it cannot name, labels the item Unknown, and the later rules do not run.

The built-in actors are Human, Claude, Codex, and Cloud agent. Unknown means the evidence is missing or disagrees. Adding an actor is a row in the catalog in `internal/actor`, plus a trailer, a session source, or a branch prefix.

| JSON `actor` | Label |
|---|---|
| `human` | Human |
| `claude` | Claude |
| `codex` | Codex |
| `cloud_agent` | Cloud agent |
| `unknown` | Unknown |

An actor id that is not in the table still renders. Its label is the id.

## Rules

### 1. Co-authored-by

weeklog reads `Co-authored-by` lines in the title and summary of a commit or a pull request. The token is case-insensitive. A commit or pull request `actor` value is read the same way when it is an email or a known agent login.

| Evidence | Actor |
|---|---|
| Mailbox at `anthropic.com` whose local part is `noreply` or contains `claude`, or the name `Claude` with no mailbox, or `Claude` plus `Opus`, `Sonnet`, or `Haiku` | Claude |
| Mailbox at `openai.com` whose local part contains `codex`, or the name `Codex`, `OpenAI Codex`, or `gpt-…-codex` | Codex |
| `cursoragent@cursor.com`, mailbox `cursor` or `cursoragent` at `cursor.com`, or the name `Cursor` or `Cursor Agent` with a `cursor.com` or `cursor.sh` mailbox or no mailbox | Cloud agent |
| GitHub login `cursoragent` or `cursor[bot]` | Cloud agent |

A person's name that merely contains "Claude", and a personal mailbox, are not Claude. A human `Co-authored-by` line is ignored. An agent weeklog does not list (for example `copilot@github.com`, a `[bot]` mailbox, or `aider.chat`) makes the item Unknown (`unrecognized-agent`). Two listed actors, or a listed actor plus an unlisted agent, make the item Unknown (`conflict`).

Session prompts are not scanned. A prompt that quotes a trailer is not a trailer.

### 2. Session linked to a commit

The item contains an agent session and a commit, and at least one stored reason is a hard or soft link. The link reasons are the same ones the linker stores: `ref:`, `sha:`, `branch:`, `lineage:`, `files overlap:`, `head branch:`, `only session`, `git branch:`, `revert:`, and `override:link`. `leftover:`, `session only`, and `override:unlink` are not links.

| Session source | Actor |
|---|---|
| `claude-code`, `claude` | Claude |
| `codex` | Codex |
| `cursor-cloud` | Cloud agent |

The source match is case-insensitive. Every known session in the item must name the same actor. A session source that is not listed, or two known sources, makes the item Unknown.

### 3. Session with no commit

When the item has no commit, the session source uses the same table as rule 2. A pull request may be present. The evidence value is `session`. A branch recorded on the session does not replace the session source: a Codex session on `cursor/fix-webhooks` is Codex.

### 4. Agent branch prefix

The first segment of a branch is compared case-insensitively and must be followed by `/`. weeklog reads the event branch and branch reasons (`branch:`, `git branch:`, `head branch:`). `refs/heads/` and `refs/remotes/<remote>/` are stripped first.

| Prefix | Actor |
|---|---|
| `cursor/` | Cloud agent |

`cursor` with no slash, and `feature/cursor-fix`, do not match. Two prefixes that name different actors make the item Unknown. This rule is how a commit or pull request on `cursor/` is Cloud agent when no session was linked to that commit.

### 5. Session and commit with no link

A session and a commit that the linker did not join, and that rule 4 did not already tag, are Unknown (`unlinked`). Pass 3 groups leftovers by repository and day, and that grouping is not a session-to-commit link. A `cursor/` branch on that commit still uses rule 4.

### 6. Human

A commit or pull request with no agent evidence is Human (`human`). The author field may be empty. This is the recorded commit or pull request, which weeklog already limited to the configured authors when the git source collected it.

### 7. Unknown

Anything else is Unknown (`no-evidence`). A ticket, review, or comment with no agent branch is Unknown.

## What the report shows

The Markdown report prints an actor line under the week summary:

```text
Actors · Human 2 · Claude 1 · Codex 1 · Cloud agent 1
```

Each item starts with the actor and the evidence value:

```text
- Actor: Claude · session-commit
```

The terminal recap, with color and without it, has an Actors section with the same counts. The recap, the Markdown summary, and `--json` count the items for each actor. `--json` also includes `actor` and `actor_evidence` on every item. `--json` sets `actor` and `actor_evidence` on every item and `summary.actors` to the counts. Zero counts are omitted. Unknown is last.

`actor_evidence` is one of `co-authored-by`, `session-commit`, `branch`, `session`, `human`, `conflict`, `unrecognized-agent`, `unlinked`, or `no-evidence`.

## Examples

A Claude trailer on a `cursor/` branch is Claude. The trailer is the first rule.

A Claude session linked to a commit on `cursor/admin-gate` is Claude. The session-to-commit link is ahead of the branch prefix.

A commit and a pull request on `cursor/webhooks`, with no session, are Cloud agent.

A `cursor-cloud` session linked to a commit is Cloud agent. A `cursor-cloud` session with no commit is Cloud agent. A `cursoragent@cursor.com` trailer on that commit is still Cloud agent, from rule 1. A `cursor-cloud` session and a Codex session linked to the same commit are Unknown.

A Codex session with no commit is Codex, including when that session recorded a `cursor/` branch.

A commit on `main` with no trailer, no session, and no agent branch is Human.

A ticket with no agent branch is Unknown.

A Claude session and a Codex session linked to the same commit are Unknown.

A Claude session and a commit grouped only by `leftover:` are Unknown.

Attribution reads stored text. Redaction runs before the event is written, and the summary is capped at 500 characters, so a trailer that was removed or truncated is not evidence.
