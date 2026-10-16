# Pre-public check

Audit of `github.com/jeremyjaymes/weeklog` on 2026-10-16, before the repository is made public. Read-only except for the wording and `.gitignore` edits in this pull request. Visibility was not changed, no tag was moved or created, and history was not rewritten.

HEAD at audit time: `be6fe8522228136010c49c8013ac85fecb765be2` (`main`).

## No live credentials

No real API token, GitHub token, Cursor key, or private key is in this history. Nothing here needs to be rotated because it was committed. History was not rewritten.

The personal Gmail address below will become public with the git history. It is not a credential.

## Scope

Scanned every ref that GitHub still has:

- `main` (86 commits, 47 of them non-merge)
- tags `v0.1.0` (`42da4f14`) and `v0.2.0` (`ccbb537`)
- every existing pull-request head (40 heads, including merged and closed)
- the only two commits not reachable from `main`: `0396418` (closed PR #63, "Load .env") and `069b1a7` (closed PR #36). There are no other branches.

Tools:

- gitleaks 8.30.1, `git log -p -U0 --all`. It scanned 45 commits. The other two non-merge commits (`90b6cd1`, `38dbfc2`) only retrigger CI and add no lines. A separate `gitleaks detect --pipe` of the two closed-PR commits found nothing.
- trufflehog 3.99.2, `trufflehog git file:///workspace`. 0 verified secrets, 8 unverified.
- A pass over every commit for `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_`, `crsr_`, `sk-`, `AKIA`/`ASIA`, Slack `xox*`, private-key blocks, webhook URLs, email addresses, `/Users/`, `/home/`, and private hostnames.
- `docs/demo.gif` (162186 bytes): no token, email, `/Users/`, or `/home/`.

GitHub's secret-scanning alerts API returned 403 for this token (`Resource not accessible by integration`). That is not a clean bill from GitHub; the scanners above are the check.

## Scanner hits

Every hit is a test canary, a redaction fixture, or documentation of a fake URL. None are live.

### gitleaks (7)

| Commit | File | Rule | Value | Verdict |
|---|---|---|---|---|
| `b68264b9` | `internal/config/config_test.go:919` | generic-api-key | `wksec_CURSOR_ref_91` | False positive. Test canary. |
| `0984b375` | `internal/config/init_test.go:229` | generic-api-key | `wksec_INIT_CANARY_9f3a` | False positive. Test canary. |
| `1e10349e` | `internal/source/process_test.go:57` | generic-api-key | `wksec_ARGV_must_miss_44bb` | False positive. Test canary. |
| `4a261607` | `testdata/secrets/corpus.json:15` | generic-api-key | `wkpass-9f3a2c1b` | False positive. Corpus fixture. |
| `089f2ab9` | `internal/config/config_test.go:380` | generic-api-key | `wksec_STDERR_c0ffee99` | False positive. Test canary. |
| `4a261607` | `internal/redact/redact_test.go:264` | generic-api-key | `0123456789abcdef` repeated | False positive. Low-entropy fixture. |
| `4a261607` | `internal/redact/redact_test.go:161` | private-key | `WEEKLOGFAKEOPENSSHKEY` between PEM markers | False positive. The body is the literal `WEEKLOGFAKEOPENSSHKEY`. |

`ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789` in `internal/source/helper_test.go` and `cmd/weeklog/image_unix_test.go` is the same kind of canary. Entropy is too low for the generic-api-key rule. Other `ghp_`, `github_pat_`, `sk-`, `xox`, and `AKIA` strings are built with `strings.Repeat` or described as prefix-plus-length in `testdata/secrets/corpus.json`. No `crsr_` value longer than the placeholder `crsr_…` appears anywhere.

### trufflehog (8 unverified, 0 verified)

| Commit | File | Raw | Verdict |
|---|---|---|---|
| `3c017922` | `internal/source/endpoint_test.go:18` | `http://user:pass@127.0.0.1` | False positive. Loopback userinfo rejected by the endpoint check. |
| `4a261607` | `docs/REDACTION.md:40` | `https://user:pass@host` | False positive. Docs example of URL userinfo. |
| `4a261607` | `internal/redact/redact_test.go:159` | `https://wkuser:wkpass-9f3a2c1b@example.com` | False positive. Redaction fixture. |
| `37801756` | `docs/security-review-2026-10.md:54` | `https://user:s3cret-token@github.com` | False positive. Named canary. |
| `4e7118ab` | `internal/sources/git/source_test.go:71` and `:458` | `https://user:s3cret-token@github.com` | False positive. Git remote canary. |
| `d7875903` | `testdata/codex/.../rollout-2026-10-10T12-00-00-....jsonl:1` | `https://user:s3cret-token@github.com` | False positive. Codex fixture. |
| `d7875903` | `internal/sources/codex/source_test.go:329` | `https://user:s3cret-token@github.com` | False positive. Same canary. |

## Personal emails, paths, and hostnames

These are not credentials. They will be public if the repository is.

### Commit author (history; not rewritten)

`Jeremy <j.vossman@gmail.com>` is the author of 40 commits (merge commits and the initial commit). `Cursor Agent <cursoragent@cursor.com>` authored the other 46. Making the repository public publishes that Gmail address in `git log`. It was not removed.

### Sample identity in the tree

`jeremy@geneseesoftware.com` is the sample address in `docs/CONFIG.md` and `internal/config/config_test.go`, next to `github = "jeremyjaymes"` and `timezone = "America/Chicago"`. It matches the copyright holder (Genesee Software Co. LLC) and the GitHub login. Left as-is. Tests assert the exact string.

Other addresses in fixtures are fictional (`alice@example.com`, `you@example.com`, `jeremy@example.com`, `claude.smith@gmail.com`, `developer@example.com`) or public agent mailboxes (`cursoragent@cursor.com`, `noreply@anthropic.com`).

### Paths

`/Users/j/Projects/otto` is the fixture home used by linker, store, Claude, and Codex testdata. The username is the single letter `j`. `/home/you` is the documented example in `docs/sources/codex.md` (and in older README revisions). No real home directory or machine hostname showed up. No `*.local` / `*.internal` host, and no RFC1918 address used as a machine.

`github.com/JeremyJaymes/otto` is the remote in `internal/sources/git/source_test.go` and a few docs (`docs/LINKING.md`, `internal/redact/redact_test.go`). `gh repo view JeremyJaymes/otto` does not resolve to a repository. Most other fixtures use `acme/otto`. Left as-is; rewriting the fixture remote is not a docs-only change.

## LICENSE and module path

`LICENSE` is the standard MIT text. Copyright line: `Copyright (c) 2026 Genesee Software Co. LLC`. The README license line matches. GitHub classifies the repository license as MIT.

`go.mod` declares `module github.com/jeremyjaymes/weeklog`. That matches the repository. The language line is `go 1.23.0` with `toolchain go1.27.2`. Go 1.27.2 is on the public download list, so `go install` can fetch that toolchain.

## Releases and GoReleaser

`v0.2.0` exists, points at `ccbb537` (merge of PR #79, 2026-10-16), and is the latest GitHub release. It is not a draft or prerelease. Assets, all present, checksums verified:

| Asset | Size | sha256 |
|---|---|---|
| `weeklog_0.2.0_darwin_amd64.tar.gz` | 6687237 | `f4f59b099df88feea782500c8358168355b26138713e127a4c0fb7d6f641409e` |
| `weeklog_0.2.0_darwin_arm64.tar.gz` | 6232296 | `eddd00c901c9b66ddcd9a3b4593d11825920622c4968428be75e5161dcdb948b` |
| `weeklog_0.2.0_linux_amd64.tar.gz` | 6590984 | `244013c21c0ac92d925c6463bdb06623a9c99a27c739acfece1ab29c5b5c69c5` |
| `weeklog_0.2.0_linux_arm64.tar.gz` | 6020841 | `f1db895aee21805feb02b7e2701e52ef2e52ba91ed9b34fefc00223e6a8a88a1` |
| `checksums.txt` | 398 | |

Each tarball contains `LICENSE`, `README.md`, and `weeklog`. The Linux amd64 binary is a static ELF and prints `weeklog version 0.2.0`. The darwin arm64 binary is Mach-O arm64. `v0.1.0` (`42da4f14`) has the same four-archive layout.

`.goreleaser.yaml` is GoReleaser v2 and is already what a public repository needs:

- `CGO_ENABLED=0`, `-trimpath`, `-X main.version={{.Version}}` (matches `var version` in `cmd/weeklog/main.go`)
- `linux` and `darwin`, `amd64` and `arm64`, `tar.gz` only
- checksums and a git changelog
- no draft flag, no private-only owner override, no token baked into the file

`.github/workflows/release.yml` runs on `v*` tags with `contents: write`, `fetch-depth: 0`, and `GITHUB_TOKEN`. That is enough for a public repository. There is no Homebrew section, which matches the README (no weeklog formula).

`main` is two commits ahead of the tag: `3780175` and `be6fe85` (PR #80, the October security review). Those commits fix `gh auth token` stderr, `git remote get-url --`, and `weeklog week --image` path handling. The v0.2.0 binaries do not contain them. `go install ...@latest` installs the tag, so it does not contain them either. This audit did not move the tag or cut another release. Cut a new tag before announcing the binaries if those fixes should be what people download.

## Install instructions

Once the repository is public, the README paths work without a private credential:

- `go install github.com/jeremyjaymes/weeklog/cmd/weeklog@latest` resolves `@latest` to `v0.2.0`. The module path is the repository root, and `cmd/weeklog` is the main package. No `GOPRIVATE` appears in the tree or the history. While the repository stays private, the public module proxy cannot see it; the README correctly does not tell anyone to use a private proxy.
- The releases-page link and the `weeklog_<version>_<os>_<arch>.tar.gz` names match the published assets. The snippet previously named `0.1.0`, which is still a real release. This pull request points the example at `0.2.0`.
- `go install ./cmd/weeklog` from a checkout does not need the module to be public.
- Homebrew: weeklog has no formula and `.goreleaser.yaml` has no `brews` block. `docs/OUTPUT.md` mentions `brew install charmbracelet/tap/freeze`, which is the public Charm tap, not a private one.

Nothing in the install docs assumes a private GitHub token, a private module proxy, or a private tap.

## Docs

Worth a trim, and trimmed in this pull request:

- `docs/research/agent-activity-source.md` said "Jeremy's preference" and "the literal token Jeremy asked for". Both now describe the one-token config without the name.

Reviewed and left:

- The Grok Bot section of that note. Grok Bot is a public Cursor product (`https://cursor.com/docs/grok-bot/security` loads). The note cites public docs. Two sentences still attribute behavior to a Cursor staff reply (2026-10-12, and the git-attribution thread around line 308) rather than a doc page. Those are source notes, not codenames. Delete them only if that citation should not be public.
- `docs/security-review-2026-10.md` is a normal review write-up. It says the tool is a personal single-user CLI. No private host, token, or internal-only name.
- Sample identity `jeremy@geneseesoftware.com` / `jeremyjaymes` / `America/Chicago` in `docs/CONFIG.md`. It is the maintainer's public identity, and tests lock the email.
- `GEN-` ticket keys in fixtures. They match the company name and are not a credential.

## Edits in this pull request

- `.gitignore` ignores `config.toml`, `.env`, `.env.*`, `*.db`, `*.sqlite`, and `*.sqlite3`. The config file holds tokens; the database holds collected events. Neither was tracked.
- README install example uses the v0.2.0 asset names.
- The two "Jeremy" asides in the research note.

## Not done

- Repository visibility is still private.
- No tag was created or moved. `v0.2.0` still points at `ccbb537`.
- History was not rewritten. `j.vossman@gmail.com` is still on 40 commits.
- No merge.
