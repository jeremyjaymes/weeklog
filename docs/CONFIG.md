# Configuration

weeklog reads a TOML file. `weeklog config check` validates that file. A literal secret is the normal form. Optional `env:`, `cmd:`, and `file:` references are resolved. The report says whether each secret resolved and never prints the value.

## Location

weeklog picks the file in this order:

1. `--config PATH` when the flag is set. A leading `~` expands to your home directory.
2. `$XDG_CONFIG_HOME/weeklog/config.toml` when `XDG_CONFIG_HOME` is set.
3. `$HOME/.config/weeklog/config.toml` on Linux and macOS, which is `~/.config/weeklog/config.toml` when `XDG_CONFIG_HOME` is unset. macOS uses `~/.config` when that variable is unset. On Windows, the OS user config directory.

`--config` is a global flag, so both of these work:

```bash
weeklog config check
weeklog --config ./config.toml config check
weeklog config check --config ./config.toml
```

## File

```toml
[identity]
emails = ["jeremy@geneseesoftware.com"]
github = "jeremyjaymes"
timezone = "America/Chicago" # IANA name; omit or set "local" for the machine zone
week_starts = "monday"       # sunday through saturday; default monday

[output]
dir = "~/journal/weeklog" # leading ~ is expanded; empty warns at check time
mode = "both"             # both, file, or terminal
template = ""             # empty uses the built-in template

[redaction]
extra_patterns = [] # regular expressions applied before events are stored
allow = []

[[source]]
type = "git"
roots = ["~/Projects"]
authors = ["jeremy@geneseesoftware.com"]

[[source]]
type = "claude-code"

[[source]]
type = "codex"

[[source]]
type = "github"
token = "cmd:gh auth token"

[[source]]
type = "linear"
# Paste your key.
token = ""
ticket_pattern = "GEN-\\d+"

[[source]]
type = "cursor-cloud"
# Paste your API key. One literal value.
token = ""

[[source]] # external plugins use the same table
type = "basecamp"
account_id = "123"
token = "cmd:op read op://dev/basecamp/token" # env:, cmd:, and file: are optional
```

### Identity

| Key | Required | Notes |
|---|---|---|
| `emails` | no | Array of strings. Blank entries are an error. |
| `github` | no | Handle, without whitespace. |
| `timezone` | no | IANA name such as `America/Chicago`. Empty or `local` means the machine zone. `utc` is accepted. |
| `week_starts` | no | `sunday`, `monday`, `tuesday`, `wednesday`, `thursday`, `friday`, or `saturday`. Default `monday`. |

### Output

| Key | Required | Notes |
|---|---|---|
| `dir` | no | Directory for weekly Markdown. A leading `~` is expanded. Unset is a warning. Used when `--out` is omitted and `mode` is `both` or `file`. |
| `mode` | no | `both`, `file`, or `terminal`. Default `both`. `both` writes `<dir>/<week>.md` and, when stdout is a terminal and `--out` is omitted, also prints the recap and a dim line such as `saved ~/…/2026-W42.md`. `file` writes the file and does not print the recap. `terminal` prints the report and does not write a file. `--out FILE` and `--out -` ignore this setting. A pipe, `NO_COLOR`, and `--plain` still print Markdown. Without `dir`, the report is printed. `--save` writes the file even when `mode` is `terminal`. |
| `template` | no | Path to a Go `text/template`. Empty selects the built-in template. A leading `~` is expanded. A relative path is resolved from the config file's directory. |

`weeklog week` writes `<dir>/<YYYY-Www>.md` when `dir` is set and `mode` is `both` or `file`. The name is the ISO week, such as `2026-W41.md`. `--save` writes that file even when `mode` is `terminal`. When `dir` is unset, `--save` writes `~/.weeklog/weeks/<YYYY-Www>.md`. That path is the fallback for `--save`. It is not a second directory setting. `--out` selects another file and cannot be combined with `--save`. `-i` does not write a file and cannot be combined with `--save` or `--image`. `--json` writes `<week>.json` in the same directory. The recap, week-over-week line, `--save` on the command, `--image`, and `-i` are described in [OUTPUT.md](OUTPUT.md).

Overwrite policy: every write replaces the weekly file. An existing file is not skipped. Text between `<!-- weeklog:notes -->` markers is copied into the replacement.

### Sources

Each `[[source]]` table needs a `type`. Built-in types and the fields `config check` accepts:

| Type | Fields |
|---|---|
| `git` | `roots` (array of paths, required), `authors` (array of strings). Each root is scanned for repositories through three directory levels. `authors` matches author emails or names; when omitted, `identity.emails` is used. |
| `claude-code` | none. Reads `$CLAUDE_CONFIG_DIR/projects` when that variable is set, and `~/.claude/projects` otherwise. |
| `codex` | none. Reads `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl` when that variable is set, and `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` otherwise. |
| `github` | `token` (secret, optional). A literal token is supported. `weeklog init` writes `token = "cmd:gh auth token"`. When `token` is unset, collect runs `gh auth token`. `endpoint` is accepted only when it is an http URL on 127.0.0.1, localhost, or ::1. |
| `linear` | `token` (secret, required). Paste your key into `token`. `ticket_pattern` (regular expression) and the same loopback `endpoint`. Collect reads issues assigned to or created by the API key's user that changed in the window, and their state history. Keys that match `ticket_pattern` are stored on `refs`. When the pattern is omitted, the built-in ticket shape is used. |
| `cursor-cloud` | `token` (secret, required). Paste one Cursor API key into `token`. Collect reads Cloud Agent runs from `https://api.cursor.com` whose `createdAt` is in the window. The assistant result text is not stored. |

Any other `type` is an external plugin. Its keys are kept. Unknown fields on a built-in type are an error. `endpoint` on `github`, `linear`, and `cursor-cloud` is the exception: tests use it to replay recorded HTTP responses against the real binary, and any value that is not loopback http is an error. It is not a setting for the public APIs. Source type names are case-sensitive (`git`, not `Git`). `config check` does not run plugins. `weeklog doctor` checks that each configured type is a built-in or a discovered plugin, and it does not resolve secrets. The plugin protocol is [PLUGIN_PROTOCOL.md](PLUGIN_PROTOCOL.md). Setup and error text for each built-in are in [SOURCES.md](SOURCES.md).

Path fields (`output.dir`, `output.template`, and `roots`) expand a leading `~`. `~user` paths are not supported.

### Redaction

`extra_patterns` is a list of regular expressions and `allow` is a list of exact strings that should not be redacted. `config check` compiles each extra pattern. Empty entries are an error. Both lists are applied when events are written. See [REDACTION.md](REDACTION.md).

## Secrets

These source fields are secrets:

- Built-in fields marked secret (`token` on `github`, `linear`, and `cursor-cloud`).
- On an external source, `token`, `secret`, `password`, `passwd`, `api_key`, and keys ending in `_token`, `_secret`, `_password`, or `_key`.

A literal value is the normal form. weeklog is a single-user local tool: paste the key into the field. `weeklog init` writes `config.toml` with mode 0600 and uses an empty placeholder, with a comment telling you to paste your key:

```toml
# Paste your key.
token = ""
```

Replace `""` with the key. An empty value is an error. `config check` reports the field as `literal` and does not print the value. The value is still redacted from stored events.

`env:`, `cmd:`, and `file:` are optional alternatives:

| Form | Resolution |
|---|---|
| anything else | The secret itself. Config check reports the field as `literal` and does not print the value. |
| `env:NAME` | The environment variable `NAME`. Unset and empty are errors. Surrounding whitespace is trimmed. |
| `cmd:<command>` | The command is run with `sh -c` (or `cmd /C` on Windows), with a 30 second limit. Standard error is discarded. Standard output is trimmed and must be non-empty and at most 1 MiB. |
| `file:<path>` | The file is read, trimmed, and must be non-empty and at most 1 MiB. A leading `~` is expanded. Relative paths are resolved from the config file's directory. |

Resolved values are not printed, logged, or included in error text. Command standard error is discarded so a failing command cannot echo a secret into the report. `weeklog config check` prints `literal` or the reference (`env:NAME`, `cmd:...`, `file:...`) and whether resolution worked. It never prints the secret value.

## `weeklog config check`

The command reads the selected file and prints a report:

- Identity, output, and redaction summary. Redaction lists show counts, not the patterns.
- One block per source. Each secret field is `resolved`, `unresolved`, or `literal`.
- `error:` and `warning:` lines.
- A final `ok`, `N errors`, `N warnings`, or both.

On Unix, a config file whose mode has a group or other read bit is a warning: `warning: config: file is group- or world-readable`. The warning does not print the file contents or any secret value.

Exit status is 0 when there are no errors. Warnings, including a group- or world-readable file, still exit 0. Exit status is 1 when the file cannot be read or parsed, or when the report contains an error.

```bash
weeklog config check
weeklog config check --config ~/.config/weeklog/config.toml
```

## `weeklog init`

`weeklog init` writes one `config.toml`. The file is the only one init writes. It does not write a `.env` file.

On a terminal, init opens a form for sources (including Cursor Cloud), literal tokens, and the output dir and mode. Pick git, Claude Code, Codex, GitHub, Linear, and Cursor Cloud. Paste tokens into the form; they are stored as literals in that file and are not printed. Set `output.dir` and `output.mode` (`both`, `file`, or `terminal`). Leave the GitHub token blank to keep `token = "cmd:gh auth token"`. Linear and Cursor Cloud need a pasted token when they are selected. Cursor Cloud is written as:

```toml
[[source]]
type = "cursor-cloud"
token = "crsr_…"
```

`--no-input`, or a stdin that is not a terminal, skips the form. init then enables the sources it finds on this machine:

| Check | Enabled when |
|---|---|
| Claude Code | `$CLAUDE_CONFIG_DIR` is a directory. When that variable is unset, `~/.claude` is a directory. |
| Codex | `$CODEX_HOME` is a directory. When that variable is unset, `~/.codex` is a directory. |
| GitHub | `gh` is on `PATH`, `gh auth status` succeeds, and `gh auth token` prints a token. The file sets `token = "cmd:gh auth token"`. The token value is not written or printed. |
| git | `~/Projects` contains at least one repository within three directory levels, the same depth the git source scans. `roots` is `["~/Projects"]`. |

The file is mode 0600. It is created with mode 0600, and the directory is created with mode 0700.

`identity.emails` and git `authors` use `git config --global user.email` when that value has no spaces. `identity.github` is the login reported by `gh auth status` when it is a GitHub login. `timezone` is `$TZ` when that name is a valid IANA zone, otherwise the machine zone when it can be named. `week_starts` is `monday`. `output.dir` is `~/journal/weeklog`. `output.mode` is `both`. The file comments the three modes: `both` writes the weekly file and prints the recap, `file` writes only, and `terminal` prints only.

Without the form, sources that are not found are omitted. Linear and Cursor cloud agents are not detected. The file includes a commented block for each: paste your key into `token`. A Cursor token is one literal value. See [Linear](sources/linear.md) and [Cursor](sources/cursor-cloud.md). The file ends with those commented blocks. Uncomment a block and paste your key into `token`. `env:`, `cmd:`, and `file:` still work on that field.

```toml
# Linear is not detected. Paste your key into token.
# [[source]]
# type = "linear"
# token = ""

# Cursor cloud agents are not detected. Paste your API key into token.
# [[source]]
# type = "cursor-cloud"
# token = ""
```

`weeklog config check` accepts the file init writes. The non-interactive file does not contain a literal token. GitHub keeps `cmd:gh auth token`. The form can write a pasted token as a literal instead.

An existing config is not overwritten without confirmation. An existing file is left unchanged unless you confirm, or unless `--force` is set when the form is skipped. On a terminal, init asks before replacing the file. Otherwise it prints the file it would write and leaves the current file in place. `--force` replaces it when the form is skipped. Printed output redacts literal tokens.

```bash
weeklog init
weeklog init --no-input
weeklog init --config ./config.toml
weeklog init --force
```

## `weeklog collect`

`weeklog collect` loads this same file, resolves optional secret references, and passes the resolved settings to each source. A literal secret is passed through as itself. It refuses to collect when config check would report an error. Warnings, including a group- or world-readable config file, are printed and collection continues. Literal secret values are redacted from stored events.

The collect window uses `identity.timezone` and `identity.week_starts`. With no window flags, collect uses the previous full week in that timezone. See the command help for `--week`, `--since`, `--until`, and `--source`. Examples and partial failure are in [COLLECT.md](COLLECT.md).
