# Configuration

weeklog reads a TOML file. `weeklog config check` validates that file and resolves secret references. It prints whether each secret resolved and never prints the value.

## Location

weeklog picks the file in this order:

1. `--config PATH` when the flag is set. A leading `~` expands to your home directory.
2. `$XDG_CONFIG_HOME/weeklog/config.toml` when `XDG_CONFIG_HOME` is set.
3. `$HOME/.config/weeklog/config.toml` on Linux and macOS. On Windows, the OS user config directory.

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
token = "env:GITHUB_TOKEN" # or "cmd:gh auth token"

[[source]]
type = "linear"
token = "env:LINEAR_API_KEY"
ticket_pattern = "GEN-\\d+"

[[source]] # external plugins use the same table
type = "basecamp"
account_id = "123"
token = "cmd:op read op://dev/basecamp/token"
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
| `dir` | no | Directory for weekly Markdown. A leading `~` is expanded. Unset is a warning. |
| `template` | no | Path to a template. Empty selects the built-in template. A leading `~` is expanded. |

### Sources

Each `[[source]]` table needs a `type`. Built-in types and the fields `config check` accepts:

| Type | Fields |
|---|---|
| `git` | `roots` (array of paths, required), `authors` (array of strings) |
| `claude-code` | none |
| `codex` | none |
| `github` | `token` (secret, optional) |
| `linear` | `token` (secret, required), `ticket_pattern` (regular expression) |

Any other `type` is an external plugin. Its keys are kept. Unknown fields on a built-in type are an error. Source type names are case-sensitive (`git`, not `Git`).

Path fields (`output.dir`, `output.template`, and `roots`) expand a leading `~`. `~user` paths are not supported.

### Redaction

`extra_patterns` is a list of regular expressions and `allow` is a list of exact strings that should not be redacted. `config check` compiles each extra pattern. Empty entries are an error. Both lists are applied when events are written. See [REDACTION.md](REDACTION.md).

## Secrets

These source fields are secrets:

- Built-in fields marked secret (`token` on `github` and `linear`).
- On an external source, `token`, `secret`, `password`, `passwd`, `api_key`, and keys ending in `_token`, `_secret`, `_password`, or `_key`.

A secret value is one of:

| Form | Resolution |
|---|---|
| `env:NAME` | The environment variable `NAME`. Unset and empty are errors. Surrounding whitespace is trimmed. |
| `cmd:<command>` | The command is run with `sh -c` (or `cmd /C` on Windows), with a 30 second limit. Standard error is discarded. Standard output is trimmed and must be non-empty and at most 1 MiB. |
| `file:<path>` | The file is read, trimmed, and must be non-empty and at most 1 MiB. A leading `~` is expanded. Relative paths are resolved from the config file's directory. |
| anything else | Allowed, and reported as a warning. Prefer a reference. |

Resolved values are not printed, logged, or included in error text. Command standard error is discarded so a failing command cannot echo a secret into the report. `weeklog config check` prints the reference (`env:GITHUB_TOKEN`, `cmd:...`, `file:...`, or `literal`) and whether resolution worked.

## `weeklog config check`

The command reads the selected file and prints a report:

- Identity, output, and redaction summary. Redaction lists show counts, not the patterns.
- One block per source. Each secret field is `resolved`, `unresolved`, or `literal`.
- `error:` and `warning:` lines.
- A final `ok`, `N errors`, `N warnings`, or both.

Exit status is 0 when there are no errors. Warnings, including literal secrets, still exit 0. Exit status is 1 when the file cannot be read or parsed, or when the report contains an error.

```bash
weeklog config check
weeklog config check --config ~/.config/weeklog/config.toml
```
