# Development and CI

Install Go 1.23 or newer, and git. From a checkout:

```bash
go test ./...
go vet ./...
go install ./cmd/weeklog
export PATH="$(go env GOPATH)/bin:$PATH"
```

`go install` writes the binary to `$(go env GOPATH)/bin`. A development build prints `weeklog version dev`. Release builds inject the version at link time:

```bash
go build -ldflags "-X main.version=v0.1.0" -o weeklog ./cmd/weeklog
```

GoReleaser passes `-X main.version={{.Version}}` for tagged releases. SQLite is `modernc.org/sqlite`, so tests and builds do not need cgo.

## Tests

```bash
go test ./...
go vet ./...
```

The end-to-end test builds the binary and runs `init`, `collect`, and `week --md` against a fixture home.

`TestFixtureHomeE2E` builds the weeklog binary and runs `weeklog init`, `weeklog collect --week 2026-W41`, and `weeklog week --week 2026-W41 --md` against a temporary home directory. Git, Claude Code, and Codex use local fixtures. GitHub and Linear replay the recorded responses under `testdata/github` and `testdata/linear` on 127.0.0.1. The test process points the binary's HTTP proxy at a closed port, so a request to the public APIs fails instead of using the network.

```bash
go test -count=1 -timeout 10m ./cmd/weeklog -run '^TestFixtureHomeE2E$'
```

The Markdown report is compared with `testdata/e2e/2026-W41.md` after the `generated_at` line is replaced with a fixed timestamp. `--week 2026-W41` and `TZ=UTC` keep the window the same on every runner. To regenerate the golden file after a deliberate report change:

```bash
WEEKLOG_UPDATE_GOLDEN=1 go test -count=1 -timeout 10m ./cmd/weeklog -run '^TestFixtureHomeE2E$'
```

Review the diff before committing it. CI runs this test as its own step on Ubuntu and macOS. The general `go test ./...` step skips it so the job does not run it twice.

CI runs golangci-lint v2.14.0, which includes staticcheck. Match that locally before opening a pull request when you change Go code:

```bash
golangci-lint run
```

## CI

[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) runs on pull requests and on pushes to `main`.

The `build-test-lint` job runs on `ubuntu-latest` and `macos-latest`:

- `go mod tidy -diff`
- `go build ./...`
- `go test` (with `-race` on Linux), skipping `TestFixtureHomeE2E`
- `TestFixtureHomeE2E` as its own step (with `-race` on Linux)
- `go vet ./...`
- golangci-lint v2.14.0
- GoReleaser snapshot on Linux (`release --snapshot --clean --skip=publish`)

The `example-python-plugin` job runs `TestTodoistExample` on Ubuntu.

macOS on pull requests is best-effort (`continue-on-error`). A macOS job that is cancelled before a runner picks it up is not covered by that setting. Pushes to `main` require macOS.

Tagged `v*` pushes run [`.github/workflows/release.yml`](../.github/workflows/release.yml). GoReleaser publishes Linux and macOS archives (`amd64` and `arm64`) only. Pull request policy is in [CONTRIBUTING.md](../CONTRIBUTING.md).
