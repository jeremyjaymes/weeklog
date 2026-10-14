# weeklog

An open-source CLI that pulls your week from pluggable sources (git, coding agents, pull requests, and tickets), links the activity into units of work, and writes a plain Markdown weekly log you own.

**Status: pre-alpha.** `weeklog version`, `weeklog init`, `weeklog config check`, `weeklog sources`, `weeklog doctor`, `weeklog collect`, `weeklog link-run`, `weeklog link`, `weeklog unlink`, `weeklog explain`, and `weeklog week` are implemented.

## Quickstart

Requires Go 1.23 or newer. From a checkout of this repository, install the command from the main package at `./cmd/weeklog`:

```bash
go install ./cmd/weeklog
```

`go install` writes the binary to `$(go env GOPATH)/bin`. Put that directory on `PATH` before running `weeklog`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
weeklog version
```

Or build a binary in the current directory:

```bash
go build -o weeklog ./cmd/weeklog
```

`weeklog version` prints `weeklog version dev` for a development build.

`weeklog init` writes `~/.config/weeklog/config.toml`, or `$XDG_CONFIG_HOME/weeklog/config.toml` when that variable is set. macOS uses `~/.config` when `XDG_CONFIG_HOME` is unset. `--config` selects another file.

init enables Claude Code when `$CLAUDE_CONFIG_DIR` is a directory, or `~/.claude` when that variable is unset. Codex uses `$CODEX_HOME` the same way, otherwise `~/.codex`. GitHub is enabled when the `gh` CLI is logged in, and git when `~/Projects` contains repositories. Sources that are missing are omitted. An existing file is left unchanged unless `--force` is set. A hand-written file has the same shape:

```toml
[identity]
emails = ["you@example.com"]
timezone = "America/Chicago"
week_starts = "monday"

[output]
dir = "~/journal/weeklog"

[[source]]
type = "git"
roots = ["~/Projects"]
authors = ["you@example.com"]

[[source]]
type = "claude-code"
```

`git` scans the `roots` directories. `claude-code` reads `$CLAUDE_CONFIG_DIR/projects`, or `~/.claude/projects` when that variable is unset. `github` reads pull requests, reviews, and issues from the GitHub GraphQL API. Setup, the config block, token access, and errors are in [GitHub](#github). `linear` reads issues and state history from the Linear GraphQL API. Setup is in [Linear](#linear). `codex` reads `$CODEX_HOME` or `~/.codex`. init writes that source when the directory is present. Collect skips it until its collector lands.

```bash
weeklog init
weeklog config check
weeklog collect
weeklog week
```

`config check` reports whether each secret reference resolved and does not print the value. `collect` reads the previous full week in the configured timezone, stores the events, and runs the linker. `week` writes that week to `~/journal/weeklog/<year>-W<week>.md` when `output.dir` is set. The file is overwritten on the next run, and text between `<!-- weeklog:notes -->` markers is kept.

Use `--week 2026-W41` on both commands for a specific ISO week. `weeklog week --out -` prints Markdown to standard output. `weeklog week --json` prints the same report as JSON.

## Build

Requires Go 1.23 or newer. Install from a checkout with the main package path:

```bash
go install ./cmd/weeklog
export PATH="$(go env GOPATH)/bin:$PATH"
weeklog version
```

`go install` writes the binary to `$(go env GOPATH)/bin`. A development build prints `weeklog version dev`. Release builds inject the version at link time:

```bash
go build -ldflags "-X main.version=v0.1.0" -o weeklog ./cmd/weeklog
```

## Config

weeklog reads `$XDG_CONFIG_HOME/weeklog/config.toml`, or `~/.config/weeklog/config.toml` when `XDG_CONFIG_HOME` is unset. `--config` selects another file.

```bash
weeklog config check
weeklog config check --config ./config.toml
```

`config check` resolves `env:`, `cmd:`, and `file:` secret references and reports whether each one resolved. It does not print secret values. See [docs/CONFIG.md](docs/CONFIG.md).

## Sources

Built-in sources are compiled into the binary. An external plugin is an executable named `weeklog-source-<name>` on `PATH` or in the plugin directory (`$XDG_DATA_HOME/weeklog/plugins`, or `~/.local/share/weeklog/plugins`).

```bash
weeklog sources
weeklog doctor
```

`sources` lists built-ins and plugins after a protocol `describe` check. `doctor` checks paths, permissions, and that each plugin speaks protocol v1. It does not print or resolve secrets. The protocol is specified in [docs/PLUGIN_PROTOCOL.md](docs/PLUGIN_PROTOCOL.md).

## GitHub

`github` collects pull requests, reviews, and issues from `https://api.github.com/graphql` for one login.

### Prerequisites

Install the `gh` CLI, run `gh auth login`, and omit `token`. Collect looks up `gh` on `PATH` and runs `gh auth token`. It keeps the first line of standard output, trimmed, and discards standard error. The child process gets `GH_PROMPT_DISABLED=1` and `GH_NO_UPDATE_NOTIFIER=1`, so a missing login fails instead of prompting.

Or set `token` to a secret reference. The github source reads a token from the environment when `token` is an `env:` reference:

```toml
token = "env:GITHUB_TOKEN"
```

The name after `env:` is the variable. `cmd:` and `file:` references work the same way as other secrets; see [docs/CONFIG.md](docs/CONFIG.md). `token = "cmd:gh auth token"` is resolved during config check, before collect starts. A non-empty `token` is the bearer token. The `gh auth token` fallback runs when `token` is omitted.

### Config

```toml
[identity]
github = "yourlogin"

[[source]]
type = "github"
token = "env:GITHUB_TOKEN"
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

With `token = "env:GITHUB_TOKEN"` set in the environment, `config check` includes a block like this and does not print the value:

```text
source[0] github
  token: resolved (env:GITHUB_TOKEN)
```

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
| `warning: source[N].token: literal secret; use an env:, cmd:, or file: reference` | The token is written inline. This is a warning. Collect still runs. |

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

## Linear

`linear` collects issues and state history from `https://api.linear.app/graphql` for the user who owns the API key.

### Prerequisites

Create a personal API key under Settings → Account → Security & access. Read access is enough. The linear source reads that key from the environment when `token` is an `env:` reference:

```toml
token = "env:LINEAR_API_KEY"
```

The name after `env:` is the variable. `cmd:` and `file:` references work the same way as other secrets; see [docs/CONFIG.md](docs/CONFIG.md). `token` is required. A literal token is allowed and reported as a warning.

### Config

```toml
[[source]]
type = "linear"
token = "env:LINEAR_API_KEY"
ticket_pattern = "GEN-\\d+"
```

The `linear` source table accepts `token` and `ticket_pattern` only. `ticket_pattern` is a regular expression. Keys it matches, including the issue identifier, are stored on each event's `refs`. Omit it and collect uses the built-in key shape (`GEN-301`, not `ISO-8601`). An empty `token = ""` is a config error. An invalid `ticket_pattern` is a config error.

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

With `token = "env:LINEAR_API_KEY"` set in the environment, `config check` includes a block like this and does not print the value:

```text
source[0] linear
  token: resolved (env:LINEAR_API_KEY)
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
| `warning: source[N].token: literal secret; use an env:, cmd:, or file: reference` | The token is written inline. This is a warning. Collect still runs. |

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

## Collect

`weeklog collect` reads the configured sources for one time window and stores the events in SQLite. With no window flags it collects the previous full week in the configured timezone. `--week 2026-W41` selects an ISO week. `--since` and `--until` select an explicit range; `--until` is exclusive.

```bash
weeklog collect
weeklog collect --week 2026-W41
weeklog collect --since 2026-10-05 --until 2026-10-12 --source git
```

`--source` can be repeated. A source that does not collect yet is skipped. A source that fails does not stop the others, and events from the sources that succeed are kept. Collect then runs the linker. Cursors are saved per configured source and are not advanced when that source fails. Ctrl-C stops the current source.

The database is `$XDG_DATA_HOME/weeklog/weeklog.db`, or `~/.local/share/weeklog/weeklog.db` when `XDG_DATA_HOME` is unset. `--db` selects another file. Resolved config secrets are redacted before events are written.

## Linker

`weeklog collect` runs the linker after sources finish. `weeklog link-run` runs it again without collecting.

Linking is deterministic and stores a reason on every link. Pass 1 is the hard-link pass. A commit joins a pull request that lists its SHA. A ticket key such as `GEN-301` joins the events that mention it in a branch, commit message, pull request title or body, session prompt, or refs. A pull request joins commits and sessions on its head branch in the same repository. A pull request joins its reviews when their source ids share a lineage. Configured `ticket_pattern` values select the key shape. When none is set, weeklog uses the built-in shape.

Pass 2 attaches an agent session that still has no hard link. When its working directory resolves to a repository, it joins commits in that repository between the session start and two hours after the session ends if they share a file (`files overlap: 3`). With no shared file, it joins those commits only when it is the only session in the window. A session also joins a branch when its text records a `git checkout` or `git switch`, or when the commits in the window share one branch. A revert joins the commit it undoes.

Pass 3 puts events that are still unlinked into leftover items grouped by repository and local day. A session with no commits is its own item. Each item is then marked shipped, in progress, explored, or abandoned.

`weeklog link` and `weeklog unlink` store a correction in the overrides table. The linker applies those rows after pass 3, including on the next `link-run` or `collect`. `weeklog explain` prints each event in the group and the stored reason. `weeklog week` shows the same reasons, including `override:link` and `override:unlink`. See [docs/LINKING.md](docs/LINKING.md).

```bash
weeklog link-run
weeklog link-run --week 2026-W41
weeklog explain 1
weeklog link linear/GEN-301 git/0123456789abcdef0123456789abcdef01234567
weeklog unlink git/0123456789abcdef0123456789abcdef01234567
```

## Week

`weeklog week` renders the previous full week. `--week`, `--since`, and `--until` select another window, using the same rules as `weeklog collect`.

```bash
weeklog week
weeklog week --week 2026-W41
weeklog week --json
weeklog week --out ~/journal/weeklog/2026-W41.md
```

The report groups linked work into Shipped, In progress, Explored, and Abandoned / stale. Commits and sessions that are not linked yet are still listed. Reviews, comments, issues, and unknown events are counted under Other activity.

Time is the sum of agent-session durations and is labeled as an estimate. An item with no session uses 15 minutes for each cluster of commits. The Markdown file starts with `week`, `generated_at`, and `weeklog_version` front matter.

`--md` is the default. `--json` writes the same structure for other tools. `--out FILE` writes that file; `-` writes to standard output. Without `--out`, a configured `output.dir` receives `<week>.md` (or `.json`). Otherwise the report is printed.

Rewriting a Markdown file keeps the manual notes between `<!-- weeklog:notes -->` markers. `output.template` replaces the built-in `text/template`. A relative template path is resolved from the config file's directory.

## Redaction

Secrets are removed from event text before it is written to SQLite. That includes known token shapes, assignment values, high-entropy tokens, and any secret resolved from the config. Rendered output is built from stored text, so those secrets are not written back out. See [docs/REDACTION.md](docs/REDACTION.md).

## Test

```bash
go test ./...
go vet ./...
```

## License

MIT. Copyright 2026 Genesee Software Co. LLC.
