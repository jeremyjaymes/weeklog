# Sources

Built-in sources are compiled into the binary. An external plugin is an executable named `weeklog-source-<name>` on `PATH` or in the plugin directory (`$XDG_DATA_HOME/weeklog/plugins`, or `~/.local/share/weeklog/plugins`). See [Plugins](PLUGINS.md). The protocol is [PLUGIN_PROTOCOL.md](PLUGIN_PROTOCOL.md).

| Source | What it reads | Setup |
|---|---|---|
| `git` | commits under `roots` | [Git](sources/git.md) |
| `claude-code` | local Claude Code transcripts | [Claude Code](sources/claude-code.md) |
| `codex` | local Codex rollout files | [Codex](sources/codex.md) |
| `github` | pull requests, reviews, and issues | [GitHub](sources/github.md) |
| `linear` | issues and state history | [Linear](sources/linear.md) |
| `cursor-cloud` | Cloud Agent runs | [Cursor](sources/cursor-cloud.md) |

`examples/weeklog-source-todoist` is a Python program that speaks the same protocol. It is an example, not a supported source. Copy `weeklog-source-todoist` onto `PATH` and configure a `todoist` source with a `token`. See [examples/weeklog-source-todoist/README.md](../examples/weeklog-source-todoist/README.md).

```bash
weeklog sources
weeklog doctor
```

`sources` lists built-in collectors and plugins after a protocol `describe` check. `doctor` checks paths, permissions, and that each plugin speaks protocol v1. It does not print or resolve secrets.

`config check` reports whether each secret reference resolved and does not print the value. Field lists for each `[[source]]` table are in [CONFIG.md](CONFIG.md).
