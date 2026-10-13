# Contributing

weeklog is pre-alpha. Most packages under `internal/` are placeholders for later work.

## Development

Install Go 1.23 or newer, then from a checkout:

```bash
go test ./...
go vet ./...
go install ./cmd/weeklog
```

CI also runs golangci-lint, which includes staticcheck. Match that locally before opening a pull request when you change Go code.

## Pull requests

Open pull requests against `main`. Keep each change focused. CI must pass on Linux and macOS: `go build`, `go test ./...`, `go vet`, and golangci-lint.

Do not tag or publish a release. Maintainers cut releases.
