# Security review, October 2026

Reviewed `main` at `ccbb537` (the short-README merge). weeklog is a personal, single-user local CLI. A literal token in `config.toml` is the intended way to store a key, so this review does not treat that as a finding and does not ask for a secret manager, encryption at rest, or multi-user hardening.

`govulncheck` was `golang.org/x/vuln v1.9.0`, run as `govulncheck ./...` with the Go 1.27.2 toolchain named in `go.mod`.

## Findings

### Low: `gh auth token` inherited stderr (fixed)

`ghAuthToken` is the collect fallback when a GitHub source has no token. The comment said stderr is discarded, and `weeklog init` does discard it, but the fallback set `cmd.Stderr` to nil. A nil stderr handle is the parent process's stderr. `gh` prints the token on stdout, which weeklog captures and does not put in an error string, so a normal run did not log the token. Anything `gh` wrote on stderr, including a wrapper that echoed the token, went to the terminal and into whatever was capturing that stream.

The fallback now sets stderr to `io.Discard`, matching the init probe. Stdout is still the only bytes that become the token. `TestGHAuthTokenDiscardsStderr` runs a fake `gh` that prints a `ghp_` value on stderr and checks that value does not appear on the parent stderr or in the returned token.

### Low: a remote name was a git option (fixed)

Collect runs `git remote get-url` with the remote name taken from `git remote`. The name was the last argument, with no `--`. `git remote get-url` treats `--push` and `--all` as its own options, so a remote whose name starts with `-` was parsed as a flag instead of a name. The arguments are not passed through a shell. The call is now `git remote get-url -- <name>`. `TestLookupRemoteDashName` uses a remote named `--push`.

### Low: `weeklog week --image` (fixed)

`freeze` is started with `exec.Command`, not a shell. The image path was the argv after `--output`. A path whose name looks like a flag, such as `-week.png`, can be parsed as another freeze option. The path is now bound to the flag as `--output=<path>`.

`freeze` opens the path itself. A symlink would be followed and the target truncated. The Markdown writer uses a temporary file and `rename`, which replaces the directory entry. `--image` now removes a symlink first, so freeze creates a new file, and it refuses a path that is a directory before freeze runs.

On failure, up to 400 characters of freeze's stdout or stderr were copied into the command error with no redaction. That text is now passed through the built-in redactor. The recap on stdin is already rendered from stored events. `TestWeekImageFreezeFailure` prints a `ghp_` token on the stub's stderr and requires `[REDACTED:github-token]` in the error.

### Informational: `golang.org/x/sys` GO-2026-5024

`govulncheck` reported one module-level advisory and no reachable vulnerability.

| | |
|---|---|
| ID | [GO-2026-5024](https://pkg.go.dev/vuln/GO-2026-5024) |
| Module | `golang.org/x/sys@v0.34.0` (indirect, via the terminal libraries) |
| Fixed in | `v0.44.0` |
| What | Integer overflow in `NewNTUnicodeString` |
| Platform | Windows only |
| Reachable | No. govulncheck reports that this module's code does not call it. |

No issue was filed. The overflow is not on a path this CLI calls, and it applies only on Windows. Bumping `x/sys` for an unreachable Windows function would be dependency churn, not a fix for a weeklog bug.

No other findings needed a GitHub issue.

## Token leakage

Severity: none, after the two error-path fixes above.

Resolved config secrets are removed before an event is written. `UpsertEvents` runs the redactor, which includes every resolved `env:`, `cmd:`, `file:`, and literal value, over every string field, then validates. The corpus test writes each sample into every string field and fails if the secret remains in a row, in the database file, or in text rendered from the stored event. `testdata/secrets/corpus.json` stores prefixes and lengths, not a contiguous token.

Collect output and the `runs` summary go through that same redactor (`publicText`) before they are printed or stored. Plugin stderr is passed through the built-in rules first, and the collect error string is passed through the config-aware redactor again, so a literal token that does not match a known shape is still removed from the line the user sees and from the audit row. `config check` reports `literal` or the reference (`env:NAME`, `cmd:...`, `file:...`) and does not print the value. `secretValue` formats as `[redacted]`. Init's printed body replaces literal tokens with `[redacted]`. The init probe for `gh auth token` discards the token bytes and does not return them.

HTTP clients put the token in the `Authorization` header, not the query string. Transport errors and GitHub or Linear GraphQL messages are scrubbed with the exact token before they are returned. Cursor, GitHub, and Linear clients do not follow redirects, so a 3xx response cannot carry the token to another host. Cursor 401 and 403 become `Cursor rejected the token` and do not include the response body.

Codex and git drop URL userinfo when they normalize a remote. The fixture URL `https://user:s3cret-token@github.com/acme/edge.git` is a canary: the Codex and git tests fail if `s3cret-token` appears on an event. The weekly recap, the saved Markdown, and `--image` are rendered from those stored fields. User notes between `<!-- weeklog:notes -->` are copied back as the user wrote them; weeklog does not treat that block as collected text.

`docs/demo.tape` records `weeklog week` against `docs/demoseed`. The seed is the fictional acme / GEN-341 week. `docs/demo-setup.sh` unsets `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `LINEAR_API_KEY`, and `LINEAR_TOKEN`, pins the prompt to `> `, and writes a config with no tokens. A string scan of `docs/demo.gif` did not find a token, a home directory, or an email address. Other `ghp_`, `sk-`, and `xox` strings in the tree are redaction rules, tests, or the corpus description.

Codex and Claude cursors stored in `source_state` are relative paths, sizes, and mtimes, not transcript bodies. That JSON is not redacted. The database file is mode 0600. A secret that existed only as a filename would remain there, next to a file the user already has on disk.

## config.toml permissions

Severity: none.

`writeConfigFile` creates the config directory with mode 0700, writes a temporary file, and `chmod`s it to 0600 before `rename`. The mode does not depend on the umask. Init tests require 0600. `config check` on Unix warns `file is group- or world-readable` when the mode has a group or other read bit (`040` or `004`). The warning text is fixed and does not include file contents. Windows skips the check because the permission bits are not the ACL. `TestCheckWarnsWhenConfigIsGroupOrWorldReadable` covers 0644, 0640, and 0604 (warn) and 0600, 0700, and 0620 (no warn). 0620 is group-writable and not group-readable; the documented check is the read bit, and the test locks that.

## Subprocess safety

Severity: none, after the git and freeze fixes above.

| Call | How it runs |
|---|---|
| Plugin `describe` / `collect` | `exec.CommandContext(path, "describe"\|"collect")`. No shell. The environment is a non-nil allowlist (`PATH`, `HOME`, `TMPDIR`, `LANG`, `TZ`, `LC_*`, `XDG_*`, and the Windows equivalents). A nil environment would inherit the parent, including tokens. Secrets travel only in the collect request on stdin. The executable and its directory must not be group- or world-writable, and must be owned by the user or root. |
| `cmd:` secret | One argv: `sh -c <command>` (`cmd /C` on Windows). The command is the user's config string, not text concatenated from an event, a URL, or another source. Stderr is discarded, stdout is capped at 1 MiB, and the command times out after 30 seconds. A failing command's error is `command exited N`, `command timed out`, or `command failed`, with no output. |
| `file:` secret | `os.Open` on the expanded path. Errors do not include the file contents. |
| `gh auth token` | `exec.CommandContext(gh, "auth", "token")`. Stderr discarded. The token is not placed in an error string. |
| `git` | `exec.CommandContext("git", args...)` with the repo as `cmd.Dir`. Log arguments are fixed format strings. The remote name is passed after `--`. |
| `freeze` | `exec.CommandContext(freeze, "--config", "base", "--language", "ansi", "--output="+path)`. |

## Dependency review

Severity: informational. See GO-2026-5024 above.

Direct modules are `toml`, `bubbletea`, `huh`, `lipgloss`, `term`, `go-isatty`, `termenv`, `cobra`, and `modernc.org/sqlite`. SQLite is pure Go (`modernc.org/sqlite v1.38.2`); the binary does not link libsqlite. HTTP calls use `net/http`. There is no separate TLS stack and no `golang.org/x/net` or `golang.org/x/crypto` requirement. govulncheck scanned 35 modules and the Go 1.27.2 standard library: 0 vulnerabilities in code that is called, 0 in imported packages, and the one unreachable Windows module advisory.

## Cursor cloud

Severity: none.

Collect builds only `GET` requests. `allowedRead` allows `/v1/me`, `/v1/agents`, and `/v1/agents/{id}/runs` where `{id}` is 1 to 200 characters from `[A-Za-z0-9_-]`. A single run, the SSE stream, secrets, repositories, usage, and any other path are refused before a request exists. The id is also `PathEscape`d. Pagination cursors are a `cursor` query parameter on those same paths, not a path. The default origin is `https://api.cursor.com`. `endpoint` is honored only for `http` on `127.0.0.1`, `localhost`, or `::1`, with no userinfo. Redirects are not followed. The token is an `Authorization: Bearer` header and is scrubbed out of transport errors.

## Path handling

Severity: none, after the `--image` symlink and flag fixes above.

`output.dir`, `output.template`, and `roots` expand a leading `~` and are cleaned. `~user` is rejected. `--out` and `--image` use the same expansion. `--image` must end in `.png` or `.svg` after cleaning; `-` is rejected because an image has to be a file.

`--save` writes `<output.dir>/<file>` when `output.dir` is set, and `~/.weeklog/weeks/<file>` when it is not. The file name comes from `FileBase`, which replaces `..` with `--` and `/` or `\` with `-`, so an ISO week or a date span (`2026-10-05..2026-10-12`) cannot leave that directory. `--out` and `--image` are the path the user named. They are not confined to `output.dir`.

Markdown and JSON are written to a temporary file in the destination directory, mode 0644, then renamed over the destination. `rename` replaces a symlink instead of following it. An existing directory is an error. `--image` now refuses a directory and unlinks a symlink before freeze creates the file. Report and image directories are created mode 0755. That matches a weekly note the user asked to write. `config.toml` and the SQLite file stay mode 0600 because they hold tokens and unredacted-until-write event text.
