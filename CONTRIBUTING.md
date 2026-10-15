# Contributing

## Development

Install Go 1.23 or newer, and git. From a checkout:

```bash
go test ./...
go vet ./...
go install ./cmd/weeklog
```

`go install` writes the binary to `$(go env GOPATH)/bin`. SQLite is `modernc.org/sqlite`, so tests and builds do not need cgo.

CI runs golangci-lint v2.14.0, which includes staticcheck. Match that locally before opening a pull request when you change Go code:

```bash
golangci-lint run
```

## Tests

```bash
go test ./...
go vet ./...
```

`TestFixtureHomeE2E` builds the weeklog binary and runs `weeklog init`, `weeklog collect --week 2026-W41`, and `weeklog week --week 2026-W41 --md` against a temporary home directory. Git, Claude Code, and Codex use local fixtures. GitHub and Linear replay the recorded responses under `testdata/github` and `testdata/linear` on 127.0.0.1. The test process points the binary's HTTP proxy at a closed port, so a request to the public APIs fails instead of using the network.

```bash
go test -count=1 -timeout 10m ./cmd/weeklog -run '^TestFixtureHomeE2E$'
```

The Markdown report is compared with `testdata/e2e/2026-W41.md` after the `generated_at` line is replaced with a fixed timestamp. `--week 2026-W41` and `TZ=UTC` keep the window the same on every runner. To regenerate the golden file after a deliberate report change:

```bash
WEEKLOG_UPDATE_GOLDEN=1 go test -count=1 -timeout 10m ./cmd/weeklog -run '^TestFixtureHomeE2E$'
```

Review the diff before committing it. CI runs this test as its own step on Ubuntu and macOS. The general `go test ./...` step skips it so the job does not run it twice.

## Plugins

A plugin is an executable named `weeklog-source-<name>`. The contract is [docs/PLUGIN_PROTOCOL.md](docs/PLUGIN_PROTOCOL.md). [examples/weeklog-source-todoist](examples/weeklog-source-todoist/README.md) is a short Python program that speaks protocol v1. It is an example, not a supported source.

Put the executable on `PATH` or in the plugin directory (`$XDG_DATA_HOME/weeklog/plugins`, or `~/.local/share/weeklog/plugins`). `weeklog doctor` checks that it answers `describe`. A `[[source]]` table with `type` set to the plugin name is configured the same way as a built-in source.

## Pull requests

Open one pull request per change, against `main`. Keep the change focused. CI must pass on Ubuntu and macOS.

Do not tag a release or publish binaries. The maintainer merges every pull request and cuts releases.
