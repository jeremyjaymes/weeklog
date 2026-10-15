# Plugin protocol v1

A weeklog source is a plugin that turns activity from one system into events. Built-in sources and external programs implement the same contract. This document is that contract.

Protocol version: **1**.

## Discovery

An external plugin is an executable named `weeklog-source-<name>`. `<name>` is the source type: it starts with a letter or digit and contains only letters, digits, `_`, and `-`. Names are case-sensitive.

weeklog searches these places, in order:

1. The plugin directory. `$XDG_DATA_HOME/weeklog/plugins` when `XDG_DATA_HOME` is set. Otherwise `~/.local/share/weeklog/plugins` on Linux and macOS, or `%USERPROFILE%\AppData\Local\weeklog\plugins` on Windows.
2. Each directory on `PATH`. Empty `PATH` entries are skipped.

The first executable for a name wins. A later copy of the same name is ignored. A plugin whose name matches a built-in (`git`, `claude-code`, `codex`, `github`, `linear`) is ignored so it cannot replace the built-in.

The file must be a regular file and, on Unix, executable. A matching file that is not executable is ignored.

On Unix, weeklog also ignores a plugin when the file or the directory that contains it is writable by the group or by other users (the mode includes `022`), or when that file or directory is owned by someone other than the current user or root. The ignore reason is `group/world-writable` or `not owned by you or root`. Windows does not apply this ownership check; discovery there only requires the `weeklog-source-<name>` name.

`weeklog sources` and `weeklog doctor` report ignored files and the reason. They do not execute them.

## Invocation

weeklog starts one process per command. There is no long-running plugin process.

```text
weeklog-source-<name> describe
weeklog-source-<name> collect
```

`describe` reads nothing. `collect` reads one JSON request from stdin. Both write to stdout and may write diagnostics to stderr.

The argument list is only the executable path and one command. weeklog does not put configuration or secrets in argv. The plugin process does not inherit weeklog's environment. It receives only:

- `PATH`, `HOME`, `TMPDIR`, `LANG`, and `TZ`
- every variable whose name starts with `LC_` or `XDG_`
- on Windows, `SystemRoot`, `USERPROFILE`, `TEMP`, and `Path` (the usual spelling of `PATH`)

Other variables, including `GITHUB_TOKEN`, `LINEAR_API_KEY`, and `AWS_*`, are not passed. Resolved secrets arrive only in the collect request on stdin.

The time limit for one invocation is 120 seconds unless the caller passes a shorter `--timeout`. A plugin that exceeds it is killed. Its stderr, if any, is redacted and attached to the error.

## describe

`describe` writes one JSON object to stdout. Pretty-printed JSON is accepted. Trailing non-whitespace data is not.

```json
{
  "protocol": 1,
  "name": "basecamp",
  "version": "0.1.0",
  "config": {
    "account_id": {"type": "string", "required": true},
    "token": {"type": "secret", "required": true}
  }
}
```

| Field | Meaning |
|---|---|
| `protocol` | Must be `1`. |
| `name` | Must equal the `<name>` in the executable. |
| `version` | Non-empty plugin version, at most 64 characters, with no control characters. |
| `config` | Map of accepted config keys. Use `{}` when the plugin takes none. |

Each config field has a `type` and an optional `required` boolean (default false).

| Type | JSON value the host will send |
|---|---|
| `string` | One string. |
| `strings` | An array of strings. |
| `secret` | One string. A literal value is the secret. `env:`, `cmd:`, and `file:` references are optional. |

`weeklog config check` uses this schema for the built-in sources. It does not execute external plugins. `weeklog doctor` runs `describe` and checks the protocol, name, and version.

## collect

stdin is one JSON object:

```json
{
  "protocol": 1,
  "since": "2026-10-05T00:00:00-05:00",
  "until": "2026-10-12T00:00:00-05:00",
  "cursor": null,
  "config": {"account_id": "123", "token": "<resolved secret>"},
  "identity": {"emails": ["jeremy@example.com"], "handles": {"github": "jeremyjaymes"}}
}
```

| Field | Meaning |
|---|---|
| `protocol` | Must be `1`. |
| `since`, `until` | RFC3339 timestamps. `until` is after `since`. |
| `cursor` | The opaque string saved from the previous run, or `null` when there is none. |
| `config` | Resolved settings for this source. Secret fields are plain strings here, not `env:` references. |
| `identity.emails` | Addresses from the weeklog config. May be empty. |
| `identity.handles` | Provider handles. `github` is the config's `identity.github` value when it is set. |

The request is at most 1 MiB. A plugin should treat `config` as private and should not write it to stdout, stderr, or logs.

stdout is JSON lines. One JSON object per line. Blank lines are ignored. A line longer than 1 MiB is discarded.

```json
{"type":"event","event":{"source":"basecamp","source_id":"msg-1","kind":"comment","ts":"2026-10-08T14:02:11Z","title":"Shipped the adapter"}}
{"type":"log","level":"warn","msg":"skipped 2 archived projects"}
{"type":"cursor","cursor":"opaque-token"}
```

### event

`event` is one activity record. Field names match the table below. Unknown JSON fields are ignored. The host validates the record; an invalid record is dropped and counted. Dropped records do not fail a run that exits 0.

| Field | Required | Notes |
|---|---|---|
| `source` | no | Plugin name. Omit it, or set it to the plugin name. A different name is dropped. The host stores the plugin name. |
| `source_id` | yes | Unique within the source. The dedupe key is `(source, source_id)`. |
| `kind` | yes | `commit`, `agent_session`, `pr`, `pr_review`, `issue`, `ticket`, `ticket_transition`, or `comment`. Any other non-empty kind is kept and later rendered under "Other". |
| `ts` | yes | Start time, RFC3339. Stored in UTC. |
| `ts_end` | no | End time. Must not be before `ts`. |
| `actor` | no | |
| `title` | no | |
| `summary` | no | Truncated to 500 characters by the host. |
| `url` | no | |
| `repo` | no | Normalized remote, such as `github.com/owner/repo`. |
| `branch` | no | |
| `cwd` | no | |
| `files` | no | Array of strings. Empty entries are removed. |
| `refs` | no | Ticket keys, PR numbers, and SHAs mentioned by the record. |
| `state` | no | `open`, `closed`, `merged`, `done`, or `abandoned`. |
| `metrics` | no | Object of finite numbers, such as `{"tokens_in": 1200}`. |
| `body_ref` | no | Pointer to raw data, usually a `file://` path. The host does not copy the raw data. |

### log

`level` is `info`, `warn`, or `error`. `warning` is accepted and stored as `warn`. `msg` is required and must not contain control characters. Log text is untrusted. Do not put secrets in it.

### cursor

`cursor` is an opaque string of at most 64 KiB. The host saves the last cursor line and sends it back on the next collect. A plugin that does not emit a cursor is run over the full window next time; dedupe covers the overlap. An empty cursor string is a saved value. Omitting the cursor line means "keep the previous cursor".

Events are kept only when the process exits 0. The host buffers them in memory until then, and it accepts at most 100000 events from one collect. A plugin that emits more fails with a protocol error, and none of those events are delivered. Invalid lines do not count toward the limit. A failed run does not advance the cursor.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. Invalid lines were dropped. |
| 1 | Failure. weeklog shows stderr after redacting secrets. |
| 2 | The configuration or the collect request was rejected. |

Any other non-zero code is a failure. stderr is redacted, collapsed to one line, and truncated.

## Built-in sources

`git`, `claude-code`, `codex`, `github`, and `linear` implement this protocol inside the weeklog process. `weeklog sources` and `weeklog doctor` describe them in process. The git collector reads commits from local repositories and describes version `0.1.0`. The claude-code collector reads local Claude Code session transcripts and describes version `0.1.0`. Its cursor records each transcript file's size and modification time. The codex collector reads local Codex rollout transcripts and describes version `0.1.0`. Its cursor records each rollout file's size and modification time. The github collector reads pull requests, reviews, and issues from the GitHub GraphQL API and describes version `0.1.0`. It uses the configured token, or `gh auth token` when the token is unset. It does not keep a cursor; the next collect reads the window again. The linear collector reads issues and state history from the Linear GraphQL API and describes version `0.1.0`. It uses the configured token. Keys that match `ticket_pattern` are stored on each event; when the pattern is omitted, the built-in ticket shape is used. It does not keep a cursor; the next collect reads the window again.

| Name | Config |
|---|---|
| `git` | `roots` (`strings`, required), `authors` (`strings`) |
| `claude-code` | none |
| `codex` | none |
| `github` | `token` (`secret`) |
| `linear` | `token` (`secret`, required), `ticket_pattern` (`string`) |

## Commands

`weeklog sources` prints every built-in and discovered plugin with the result of `describe`. It exits 0 when the list was printed, even if a plugin failed describe.

`weeklog doctor` checks the config file path, the plugin directory, ignored plugin files, and `describe` for every source. A missing config file or plugin directory is a warning. A protocol failure, an unusable plugin directory, or a configured source that is not installed is an error. doctor exits 0 when there are no errors. It does not resolve secrets and does not print source settings.

Both commands accept `--timeout` (default `120s`).

`weeklog collect` runs `collect` for each configured source. The default window is the previous full week in the configured timezone. `--week YYYY-Www` selects an ISO week, and `--since` with `--until` selects an explicit range (`--until` is exclusive). `--source` limits the run to configured sources and can be repeated. `--timeout` limits each source. `--db` selects the SQLite file; the default is `$XDG_DATA_HOME/weeklog/weeklog.db`, or `~/.local/share/weeklog/weeklog.db` when `XDG_DATA_HOME` is unset.

A source whose collector is not implemented yet is skipped and listed. A source that fails does not stop the others. Events from sources that succeed are stored, then the linker runs. The cursor is saved only after a source succeeds. Cancelling the command stops the current source and does not start another. The command exits 0 when every selected source succeeded or was skipped. It exits 1 when a source fails or linking fails; events already stored are kept.

## Example

This program answers `describe` and emits one collect event. It is illustrative. The program in `examples/weeklog-source-todoist` is a working Python plugin for the same protocol. It is an example, not a supported source.

```python
#!/usr/bin/env python3
import json, sys

def describe():
    json.dump({
        "protocol": 1,
        "name": "example",
        "version": "0.1.0",
        "config": {"token": {"type": "secret", "required": True}},
    }, sys.stdout)
    sys.stdout.write("\n")

def collect():
    req = json.load(sys.stdin)
    if req.get("protocol") != 1:
        sys.stderr.write("protocol must be 1\n")
        return 2
    if not req.get("config", {}).get("token"):
        sys.stderr.write("token is required\n")
        return 2
    json.dump({"type": "event", "event": {
        "source_id": "example-1",
        "kind": "comment",
        "ts": req["since"],
        "title": "Example event",
    }}, sys.stdout)
    sys.stdout.write("\n")
    json.dump({"type": "cursor", "cursor": "example-cursor"}, sys.stdout)
    sys.stdout.write("\n")
    return 0

def main():
    if len(sys.argv) != 2 or sys.argv[1] not in ("describe", "collect"):
        sys.stderr.write("usage: describe | collect\n")
        return 1
    if sys.argv[1] == "describe":
        describe()
        return 0
    return collect()

if __name__ == "__main__":
    sys.exit(main())
```

Install it as an executable named `weeklog-source-example` on `PATH` or in the plugin directory. `weeklog doctor` should report `source example: ok`.
