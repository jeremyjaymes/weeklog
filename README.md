# weeklog

An open-source CLI that pulls your week from pluggable sources (git, coding agents, pull requests, and tickets), links the activity into units of work, and writes a plain Markdown weekly log you own.

**Status: pre-alpha.** `weeklog version`, `weeklog config check`, `weeklog sources`, `weeklog doctor`, `weeklog collect`, and `weeklog link-run` are implemented.

## Build

Requires Go 1.23 or newer.

```bash
go install ./cmd/weeklog
weeklog version
```

A development build prints `weeklog version dev`. Release builds inject the version at link time:

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

## Redaction

Secrets are removed from event text before it is written to SQLite. That includes known token shapes, assignment values, high-entropy tokens, and any secret resolved from the config. Rendered output is built from stored text, so those secrets are not written back out. See [docs/REDACTION.md](docs/REDACTION.md).

## Test

```bash
go test ./...
go vet ./...
```

## License

MIT. Copyright 2026 Genesee Software Co. LLC.
