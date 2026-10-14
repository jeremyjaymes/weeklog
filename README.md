# weeklog

An open-source CLI that pulls your week from pluggable sources (git, coding agents, pull requests, and tickets), links the activity into units of work, and writes a plain Markdown weekly log you own.

**Status: pre-alpha.** `weeklog version`, `weeklog config check`, `weeklog sources`, `weeklog doctor`, `weeklog collect`, `weeklog link-run`, and `weeklog week` are implemented.

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

Create `~/.config/weeklog/config.toml`. macOS uses that path when `XDG_CONFIG_HOME` is unset. `--config` selects another file.

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

`git` scans the `roots` directories. `claude-code` reads `$CLAUDE_CONFIG_DIR/projects`, or `~/.claude/projects` when that variable is unset. `github` reads pull requests, reviews, and issues from the GitHub GraphQL API. It uses the `token` setting, and runs `gh auth token` when `token` is unset. `codex` and `linear` are recognized and skipped until their collectors land.

```bash
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

```bash
weeklog link-run
weeklog link-run --week 2026-W41
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
