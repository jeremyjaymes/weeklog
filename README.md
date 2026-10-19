# weeklog

weeklog is a CLI that reads a week of work from pluggable sources, links that activity into units of work, and writes a Markdown log.

![weeklog week recap for the fictional 2026-W41 fixture](docs/demo.gif)

`docs/demo.tape` records `weeklog week --week 2026-W41` against that fixture. It uses no tokens and no personal activity. From the repository root, regenerate the GIF with [VHS](https://github.com/charmbracelet/vhs), plus `ttyd` and `ffmpeg` on `PATH`:

```bash
vhs docs/demo.tape
```

## Install

Requires Go 1.23 or newer.

```bash
go install github.com/jeremyjaymes/weeklog/cmd/weeklog@latest
export PATH="$(go env GOPATH)/bin:$PATH"
```

From a checkout of this repository, `go install ./cmd/weeklog` writes the same binary to `$(go env GOPATH)/bin`.

Release binaries are macOS and Linux only (`amd64` and `arm64`). There is no Windows archive. Download a tarball from the [latest release](https://github.com/jeremyjaymes/weeklog/releases/latest). Names are `weeklog_<version>_<os>_<arch>.tar.gz`: `<os>` is `darwin` or `linux`, and `<version>` is the release tag without the leading `v`. The example below uses the current release, v0.2.1.

```bash
VERSION=0.2.1
tar -xzf "weeklog_${VERSION}_linux_amd64.tar.gz"
install weeklog "$HOME/.local/bin"
```

## Quickstart

```bash
weeklog init
weeklog config check
weeklog collect
weeklog week
```

## Docs

- [Configuration](docs/CONFIG.md)
- [Sources](docs/SOURCES.md), including the [Cursor cloud source](docs/sources/cursor-cloud.md)
- [Plugins](docs/PLUGINS.md) and the [plugin protocol](docs/PLUGIN_PROTOCOL.md)
- [Collect](docs/COLLECT.md)
- [Output](docs/OUTPUT.md) — modes, `--save`, `--image`, and interactive `-i`
- [Linking](docs/LINKING.md)
- [Attribution](docs/ATTRIBUTION.md)
- [Redaction](docs/REDACTION.md)
- [Development and CI](docs/DEVELOPMENT.md)

## License

MIT. Copyright 2026 Genesee Software Co. LLC.
