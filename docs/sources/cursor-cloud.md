# Cursor

See [Sources](../SOURCES.md).

`cursor-cloud` collects Cloud Agent runs from `https://api.cursor.com`. weeklog serves the collector as `weeklog-source-cursor-cloud`. A program of that name on `PATH` cannot replace the built-in.

### Prerequisites

Create an API key under Cursor Dashboard → API Keys, or use a service-account key from team settings. Paste that one key into `token`. The key can also create and delete agents. Collect never calls those routes.

### Config

```toml
[[source]]
type = "cursor-cloud"
# Paste your API key. One literal value.
token = ""
```

The `cursor-cloud` source table accepts `token` only. An empty `token = ""` is a config error until the key is pasted. `env:`, `cmd:`, and `file:` references are optional; see [CONFIG.md](../CONFIG.md).

Collect calls `GET /v1/me`, `GET /v1/agents`, and `GET /v1/agents/{id}/runs`. It does not call create, cancel, delete, secrets, repositories, usage, a single run, or the SSE stream. Each kept run becomes one `agent_session`. `source_id` is `{agentId}/{runId}`. The window includes `since` and excludes `until`, using the run's `createdAt`. An agent whose `updatedAt` is at or after `since` is read, including an agent updated after `until`, because that agent can still hold an in-window run. The assistant result text is not stored. `summary` is omitted. The source does not store a cursor; the next collect reads the window again.

`CREATING` and `RUNNING` are `open`. `FINISHED` is `done`. `ERROR`, `CANCELLED`, `EXPIRED`, and any other status are `abandoned`. The actor is `userEmail` from `/v1/me` when the key has one. A service-account key has no `userEmail`. Collect logs a warning and continues with no actor. Requests stay at or under 20 per minute. On HTTP 429, collect honors `Retry-After` when it is present and otherwise waits 1s, then 2s, then 4s. If the remaining time cannot finish the window, collect fails and stores none of that source's events.

### Token access

The call sends `Authorization: Bearer <token>`. The token is not written on the query string, and it is not included in error text.

- HTTP 401 and HTTP 403 are reported as `Cursor rejected the token`.
- HTTP 429 that cannot be retried inside the collect timeout is reported as `Cursor rate limit cannot finish the window`.

Resolved config secrets are still redacted when events are written.

### Verify

```bash
weeklog config check
weeklog collect --source cursor-cloud
```

With the key pasted into `token`, `config check` includes a block like this and does not print the value:

```text
source[0] cursor-cloud
  token: literal
```

The index is the source's position in the file.

`weeklog collect --source cursor-cloud` collects the previous full week, stores the events, and runs the linker. Success includes:

```text
source[0] cursor-cloud: ok (3 events)
link: ok
```

The count is the runs kept for the window. `--week 2026-W41` selects an ISO week. The type name is case-sensitive. A `cursor-cloud` session is Cloud agent. See [ATTRIBUTION.md](../ATTRIBUTION.md).

### Common errors

`weeklog config check` exits 1 and collect does not start when the file has an error:

| Message | Cause |
|---|---|
| `error: source[N].token: required field is missing` | The `cursor-cloud` block has no `token`. |
| `error: source[N].token: environment variable CURSOR_API_KEY is unset` | `env:CURSOR_API_KEY` is set and the variable is missing. An empty variable says `is empty`. |
| `error: source[N].token: must not be empty` | `token = ""`. |
| `error: source[N].token: command exited 1` | `token = "cmd:..."` and the command fails. Empty output says `command produced empty output`. A command that runs longer than 30 seconds says `command timed out`. |
| `error: source[N].org: unknown field for cursor-cloud` | Any source key other than `token`. |

`weeklog collect --source cursor-cloud` exits before collection with `source cursor-cloud is not configured` when that block is missing.

A collector failure is one line, and the other sources still run. The command exits 1:

```text
source[0] cursor-cloud: failed (cursor-cloud: collect: token is unset)
```

| Message inside `failed` | Cause |
|---|---|
| `cursor-cloud: collect: token is unset` | `token` is missing, empty, or only whitespace after resolution. |
| `cursor-cloud: collect: token must be a string` | `token` was not a string. |
| `cursor-cloud: collect: token contains whitespace or control characters` | The resolved token contains a space or a control character. The token is not printed. |
| `cursor-cloud: collect: protocol must be 1` | The collect request asked for another protocol version. |
| `cursor-cloud: collect: Cursor rejected the token` | HTTP 401 or HTTP 403. |
| `cursor-cloud: collect: Cursor rate limit cannot finish the window` | HTTP 429, or the 20 requests per minute budget, cannot finish inside the collect timeout. |
| `cursor-cloud: collect: Cursor returned HTTP 502` | Any other non-success HTTP status, with that status in the message. |
| `cursor-cloud: collect: Cursor request failed: ...` | The request did not complete. The token text is replaced with `[REDACTED]`. |
| `cursor-cloud: collect: Cursor agents exceeded 20 pages` | The agent list returned more than 20 pages. |
| `cursor-cloud: collect: Cursor runs for bc-… exceeded 20 pages` | That agent has more than 20 pages of runs. |

A malformed agent or run is a warning, and the source can still succeed. One malformed agent prints `source[N] cursor-cloud: warn: skipped 1 malformed agent`. One malformed run prints `source[N] cursor-cloud: warn: skipped 1 malformed run`. Counts above one use the plural. A service-account key prints `source[N] cursor-cloud: warn: service-account key has no userEmail`.

Design notes for this collector are in [research/agent-activity-source.md](../research/agent-activity-source.md).
