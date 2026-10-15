# weeklog-source-todoist

Example weeklog plugin written in Python. It is not a supported source. It shows that a plugin in another language can speak [protocol v1](../../docs/PLUGIN_PROTOCOL.md).

The executable name is `weeklog-source-todoist`. Put this directory on `PATH`, or copy the executable into the plugin directory (`$XDG_DATA_HOME/weeklog/plugins`, or `~/.local/share/weeklog/plugins`).

```bash
chmod +x weeklog-source-todoist
export PATH="$PWD:$PATH"
weeklog doctor
```

`doctor` runs `describe`. It does not call Todoist and it does not print the token.

## Config

```toml
[[source]]
type = "todoist"
token = "env:TODOIST_API_TOKEN"
```

`token` is a Todoist API token. weeklog resolves `env:`, `cmd:`, and `file:` references and sends the value only in the collect request on stdin. `api_base` is optional. It defaults to `https://api.todoist.com`.

Collect calls `GET /api/v1/tasks/completed/by_completion_date` for the requested window and emits one `ticket` event per completed task on that page. A `next_cursor` from Todoist is saved and sent back on the next run. Tasks outside the window are skipped.

weeklog redacts event text before it is written to SQLite. This plugin does not redact, and it does not log the token.

CI runs this executable through the Go subprocess adapter against a mock of that HTTP endpoint.
