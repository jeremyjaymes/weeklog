# GitHub

See [Sources](../SOURCES.md).

`github` collects pull requests, reviews, and issues from `https://api.github.com/graphql` for one login.

### Prerequisites

Install the `gh` CLI, run `gh auth login`, and omit `token`. Collect looks up `gh` on `PATH` and runs `gh auth token`. It keeps the first line of standard output, trimmed, and discards standard error. The child process gets `GH_PROMPT_DISABLED=1` and `GH_NO_UPDATE_NOTIFIER=1`, so a missing login fails instead of prompting.

Or set `token`. A literal token is supported and is not a warning. `env:`, `cmd:`, and `file:` references are optional; see [CONFIG.md](../CONFIG.md). `weeklog init` writes `token = "cmd:gh auth token"` when `gh` is authenticated. That command is resolved during config check, before collect starts. A non-empty `token` is the bearer token. The `gh auth token` fallback runs when `token` is omitted.

```toml
token = "cmd:gh auth token"
```

### Config

```toml
[identity]
github = "yourlogin"

[[source]]
type = "github"
token = "cmd:gh auth token"
```

`identity.github` is the login. Omit it and collect uses the authenticated viewer's login. The login is 1–39 characters: ASCII letters, digits, and single hyphens, and it cannot start or end with a hyphen.

The `github` source table accepts `token` only. Omit `token` to use the `gh auth token` fallback above. An empty `token = ""` is a config error. Collect searches for that login as a pull request author, a reviewer (`reviewed-by`), an issue author, and an issue assignee. Each event's repository is the `owner/name` GitHub returns for that result. `org`, `repos`, and `user` are unknown source fields.

A pull request is kept when it was opened, closed, or merged in the window, or when the login submitted a review in the window. Those reviews are separate events. An issue is kept when the login authored it or is assigned to it, and it was opened, closed, or updated in the window. The window includes `since` and excludes `until`. The source does not store a cursor; the next collect reads the window again.

### Token access

The call sends `Authorization: Bearer <token>`. For the pull request, review, and issue fields this source reads:

- A classic personal access token needs the `public_repo` scope to read public repositories, and the `repo` scope to read private repositories.
- A fine-grained personal access token includes read access to public repositories. Private repositories need read access to Pull requests and Issues on those repositories.

Those scopes are GitHub's requirement for these queries, from [Forming calls with GraphQL](https://docs.github.com/en/graphql/guides/forming-calls-with-graphql). HTTP 401, and HTTP 403 whose body does not contain `rate limit`, are reported as `GitHub rejected the token`. A GraphQL `errors` entry on a successful HTTP status is reported as that entry's first message, with the token text replaced by `[REDACTED]`.

### Verify

```bash
weeklog config check
weeklog collect --source github
```

With `token = "cmd:gh auth token"`, `config check` includes a block like this and does not print the value:

```text
source[0] github
  token: resolved (cmd:gh auth token)
```

A literal token is reported as `token: literal`. The value is not printed.

The index is the source's position in the file. When `token` is omitted, that secret line is absent and `gh auth token` runs during collect.

`weeklog collect --source github` collects the previous full week, stores the events, and runs the linker. Success includes:

```text
source[0] github: ok (3 events)
link: ok
```

The count is the pull requests, reviews, and issues kept for the window. `--week 2026-W41` selects an ISO week. The type name is case-sensitive.

### Common errors

`weeklog config check` exits 1 and collect does not start when the file has an error:

| Message | Cause |
|---|---|
| `error: source[N].token: environment variable GITHUB_TOKEN is unset` | `env:GITHUB_TOKEN` is set and the variable is missing. An empty variable says `is empty`. |
| `error: source[N].token: must not be empty` | `token = ""`. |
| `error: source[N].token: command exited 1` | `token = "cmd:gh auth token"` and the command fails. Empty output says `command produced empty output`. A command that runs longer than 30 seconds says `command timed out`. |
| `error: source[N].org: unknown field for github` | Any source key other than `token`, including `org`, `repos`, and `user`. |
| `error: identity.github: must not contain whitespace` | The login contains whitespace or a control character. |

`weeklog collect --source github` exits before collection with `source github is not configured` when that block is missing.

A collector failure is one line, and the other sources still run. The command exits 1:

```text
source[0] github: failed (github: collect: token is unset and gh auth token is unavailable)
```

| Message inside `failed` | Cause |
|---|---|
| `github: collect: token is unset and gh auth token is unavailable` | `token` is omitted, and `gh` is missing from `PATH`, `gh auth token` fails, or it prints an empty token. |
| `github: collect: token contains whitespace or control characters` | The resolved token contains a space or a control character. |
| `github: collect: github login is invalid` | `identity.github`, or the viewer login, fails the login rules above. The login is not printed. |
| `github: collect: GitHub rejected the token` | HTTP 401, or HTTP 403 whose body does not contain `rate limit`. |
| `github: collect: GitHub rate limit exceeded` | HTTP 403 whose body contains `rate limit`. |
| `github: collect: GitHub returned HTTP 502` | Any other non-success HTTP status, with that status in the message. |
| `github: collect: GitHub request failed: ...` | The request did not complete. The token text is replaced with `[REDACTED]`. |
| `github: collect: GitHub search exceeded 20 pages` | Search returned more than 20 pages. |
| `github: collect: GitHub reviews for owner/repo#N exceeded 20 pages` | That pull request has more than 20 pages of reviews. The commit equivalent says `GitHub commits for owner/repo#N exceeded 20 pages`. |

A malformed search node is a warning, and the source can still succeed. One malformed pull request prints `source[N] github: warn: skipped 1 malformed pull request`. One malformed issue prints `source[N] github: warn: skipped 1 malformed issue`. One node of another type prints `source[N] github: warn: skipped 1 unexpected GitHub result`. Counts above one use the plural.
