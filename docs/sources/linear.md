# Linear

See [Sources](../SOURCES.md).

`linear` collects issues and state history from `https://api.linear.app/graphql` for the user who owns the API key.

### Prerequisites

Create a personal API key under Settings → Account → Security & access. Read access is enough. Paste your key into `token`.

### Config

```toml
[[source]]
type = "linear"
# Paste your key.
token = ""
ticket_pattern = "GEN-\\d+"
```

The `linear` source table accepts `token` and `ticket_pattern` only. `ticket_pattern` is a regular expression. Keys it matches, including the issue identifier, are stored on each event's `refs`. Omit it and collect uses the built-in key shape (`GEN-301`, not `ISO-8601`). An empty `token = ""` is a config error until the key is pasted. An invalid `ticket_pattern` is a config error. `env:`, `cmd:`, and `file:` references are optional; see [CONFIG.md](../CONFIG.md).

Collect loads issues assigned to or created by the authenticated user whose `updatedAt` falls in the window, including archived issues. Each issue becomes a `ticket` event with its key, title, state, url, and `branchName`. State changes in that issue's history become `ticket_transition` events. The window includes `since` and excludes `until`. The source does not store a cursor; the next collect reads the window again.

A completed issue is `done`. A canceled issue is `abandoned`. Other workflow states are `open`.

### Token access

The call sends `Authorization: <token>` with the raw API key. Linear personal API keys are not sent as `Bearer`. The token is not written on the query string, and it is not included in error text.

- HTTP 401, and HTTP 403 whose body does not contain `rate limit`, are reported as `Linear rejected the token`.
- HTTP 429, HTTP 403 whose body contains `rate limit`, and a GraphQL error with code `RATELIMITED` or a message containing `rate limit`, are reported as `Linear rate limit exceeded`.
- A GraphQL `errors` entry on HTTP 200 or HTTP 400 that is not an auth or rate-limit error is reported as that entry's first message, with the token text replaced by `[REDACTED]`.

Resolved config secrets are still redacted when events are written. The collector does not rewrite issue text.

### Verify

```bash
weeklog config check
weeklog collect --source linear
```

With the key pasted into `token`, `config check` includes a block like this and does not print the value:

```text
source[0] linear
  token: literal
```

The index is the source's position in the file. `ticket_pattern` is not a secret and is not listed there.

`weeklog collect --source linear` collects the previous full week, stores the events, and runs the linker. Success includes:

```text
source[0] linear: ok (3 events)
link: ok
```

The count is the tickets and state transitions kept for the window. `--week 2026-W41` selects an ISO week. The type name is case-sensitive. A git commit that mentions the same key, such as `GEN-301`, is linked to that ticket.

### Common errors

`weeklog config check` exits 1 and collect does not start when the file has an error:

| Message | Cause |
|---|---|
| `error: source[N].token: required field is missing` | The `linear` block has no `token`. |
| `error: source[N].token: environment variable LINEAR_API_KEY is unset` | `env:LINEAR_API_KEY` is set and the variable is missing. An empty variable says `is empty`. |
| `error: source[N].token: must not be empty` | `token = ""`. |
| `error: source[N].token: command exited 1` | `token = "cmd:..."` and the command fails. Empty output says `command produced empty output`. A command that runs longer than 30 seconds says `command timed out`. |
| `error: source[N].ticket_pattern: invalid regular expression` | `ticket_pattern` does not compile. |
| `error: source[N].org: unknown field for linear` | Any source key other than `token` and `ticket_pattern`. |

`weeklog collect --source linear` exits before collection with `source linear is not configured` when that block is missing.

A collector failure is one line, and the other sources still run. The command exits 1:

```text
source[0] linear: failed (linear: collect: token is unset)
```

| Message inside `failed` | Cause |
|---|---|
| `linear: collect: token is unset` | `token` is missing, empty, or only whitespace after resolution. |
| `linear: collect: token must be a string` | `token` was not a string. |
| `linear: collect: token contains whitespace or control characters` | The resolved token contains a space or a control character. The token is not printed. |
| `linear: collect: ticket_pattern is invalid` | `ticket_pattern` does not compile. Config check reports this first when the file is loaded through `weeklog collect`. |
| `linear: collect: Linear rejected the token` | HTTP 401, HTTP 403 whose body does not contain `rate limit`, or a GraphQL authentication error. |
| `linear: collect: Linear rate limit exceeded` | HTTP 429, HTTP 403 whose body contains `rate limit`, or a GraphQL rate-limit error. |
| `linear: collect: Linear returned HTTP 502` | Any other non-success HTTP status, with that status in the message. |
| `linear: collect: Linear request failed: ...` | The request did not complete. The token text is replaced with `[REDACTED]`. |
| `linear: collect: Linear issues exceeded 20 pages` | The issue query returned more than 20 pages. |
| `linear: collect: Linear history for GEN-301 exceeded 20 pages` | That issue has more than 20 additional pages of history. |

A malformed issue is a warning, and the source can still succeed. One malformed issue prints `source[N] linear: warn: skipped 1 malformed issue`. One node of another type prints `source[N] linear: warn: skipped 1 unexpected Linear result`. One malformed history entry prints `source[N] linear: warn: skipped 1 malformed history entry`. Counts above one use the plural.
