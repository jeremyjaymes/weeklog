# End-to-end week report

`2026-W41.md` is the Markdown report from `TestFixtureHomeE2E`.

The test builds `./cmd/weeklog` and runs `init`, then `collect --week 2026-W41`, then `week --week 2026-W41 --md` in a temporary home. The home contains the Claude Code and Codex fixtures from `testdata/`, a git repository under `~/Projects/otto`, and a fake `gh`. GitHub and Linear are served from `testdata/github` and `testdata/linear` on 127.0.0.1.

`generated_at` in the golden file is `1970-01-01T00:00:00Z`. The test replaces the real timestamp with that line before comparing. The rest of the file is the report the binary wrote.

Regenerate it from the repository root after a deliberate change to the report:

```bash
WEEKLOG_UPDATE_GOLDEN=1 go test -count=1 -timeout 10m ./cmd/weeklog -run '^TestFixtureHomeE2E$'
```

Run the same command without `WEEKLOG_UPDATE_GOLDEN` and review the diff before committing.
