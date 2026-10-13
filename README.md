# weeklog

An open-source CLI that pulls your week from pluggable sources (git, coding agents, pull requests, and tickets), links the activity into units of work, and writes a plain Markdown weekly log you own.

**Status: pre-alpha.** The repository is a skeleton. `weeklog version` is the only command implemented so far.

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

## Test

```bash
go test ./...
go vet ./...
```

## License

MIT. Copyright 2026 Genesee Software Co. LLC.
