# Codex

See [Sources](../SOURCES.md).

`codex` collects agent sessions from Codex rollout files on the local machine.

### Prerequisites

Use Codex at least once so it writes a session. The collector reads `$CODEX_HOME` when that variable is set, and `~/.codex` otherwise. A leading `~` in `CODEX_HOME` is expanded. `~user` paths are not supported. No token is required.

### Config

```toml
[[source]]
type = "codex"
```

The `codex` source table accepts no keys. `roots`, `token`, and any other source key are unknown fields.

Collect reads `sessions/YYYY/MM/DD/rollout-*.jsonl` under that directory. Other files, including `history.jsonl`, are left unread. Each rollout file is one `agent_session`. The session id is the `id` in `session_meta`, then `session_id`, then the id in the filename. The window includes `since` and excludes `until`, compared to the session start. The start is the `session_meta` timestamp when it can be parsed, and otherwise the earliest line timestamp. The cursor records each file's size and modification time. A file whose fingerprint still matches is read again only when its session start lies in the requested window.

### Sessions

The on-disk format is not a public schema. The parser keeps the fields it understands and skips lines it does not. A line that is not a JSON object is counted and skipped. The session is still emitted.

A session event uses:

- **Title and summary.** The first user prompt. That is an `event_msg` of type `user_message`, or a `response_item` message with role `user`, whichever appears first. Developer and assistant text are not the title. The title is the first line, at most 200 characters. The summary is the prompt, at most 500 characters.
- **Working directory and branch.** `cwd` and `git.branch` from `session_meta`. A later `turn_context` supplies `cwd` only when `session_meta` did not.
- **Repository.** `git.repository_url` from `session_meta`, normalized to `host/owner/name`. Userinfo in the URL is removed, so a token in the URL is not stored. `git@github.com:owner/repo.git` becomes `github.com/owner/repo`.
- **Files.** Paths from `apply_patch` function calls and custom tool calls (`*** Add File:`, `*** Update File:`, `*** Delete File:`, and `*** Move to:`), and the file keys on an `event_msg` of type `patch_apply_end`. Paths under `cwd` are stored relative to it. Shell and `exec_command` text is not treated as a file list.
- **Tokens.** The latest `token_count` event's `total_token_usage`. `input_tokens` is `tokens_in`. `output_tokens` is `tokens_out`. Cached input is already included in `input_tokens` and is not added again.
- **body_ref.** A `file://` URL of the rollout file. The file is not copied.

A session with no timestamp is skipped. A rollout with no `session_meta` id still uses the filename id.

### Verify

```bash
weeklog config check
weeklog collect --source codex
```

`config check` prints the source block and no secret line, because codex has no secret fields:

```text
source[0] codex
ok
```

The index is the source's position in the file.

`weeklog collect --source codex` collects the previous full week, stores the events, and runs the linker. Success includes:

```text
source[0] codex: ok (2 events)
link: ok
```

The count is the sessions whose start falls in the window. `--week 2026-W41` selects an ISO week. The type name is case-sensitive. A missing Codex home is not a failure. Collect warns and reports zero events:

```text
source[0] codex: warn: codex home not found: /home/you/.codex
source[0] codex: ok (0 events)
link: ok
```

One unreadable line prints `source[N] codex: warn: skipped 1 unreadable session line`, and the ok line includes `, 1 dropped`. One session with no timestamp prints `source[N] codex: warn: skipped 1 session with no timestamp`. Counts above one use the plural.

### Common errors

`weeklog config check` exits 1 and collect does not start when the file has an error:

| Message | Cause |
|---|---|
| `error: source[N].token: unknown field for codex` | Any source key other than `type`, including `token` and `roots`. |

`weeklog collect --source codex` exits before collection with `source codex is not configured` when that block is missing.

A collector failure is one line, and the other sources still run. The command exits 1:

```text
source[0] codex: failed (codex: collect: expand "~nobody/codex": ~user paths are not supported)
```

| Message inside `failed` | Cause |
|---|---|
| `codex: collect: expand "~nobody/codex": ~user paths are not supported` | `CODEX_HOME` is a `~user` path. |
| `codex: collect: cannot find home directory: ...` | `CODEX_HOME` is unset and the home directory cannot be resolved. |
| `codex: collect: cannot read codex home: PATH` | The Codex home exists but cannot be read. |
| `codex: collect: cannot read sessions: PATH` | `sessions` exists but cannot be read. |

These are warnings. Collect still succeeds:

| Message | Cause |
|---|---|
| `codex home not found: PATH` | `$CODEX_HOME`, or `~/.codex`, is missing. |
| `codex home is not a directory: PATH` | That path is a file. |
| `sessions directory not found: PATH` | The Codex home has no `sessions` directory. |
| `sessions path is not a directory: PATH` | `sessions` is a file. |
| `cannot read sessions: PATH` | A year or month directory under `sessions` cannot be read. |
| `cannot read day: PATH` | One day directory cannot be read. |
| `cannot read session: PATH` | One rollout file cannot be read. |
| `session line exceeds 4 MiB: ID` | One line is longer than 4 MiB. That file is not recorded in the cursor. |
| `ignored unreadable cursor` | The saved cursor is not the collector's cursor document. Collect reads the files again. |
| `cursor exceeds 64 KiB; keeping the previous cursor` | The new cursor is larger than the protocol limit. |
