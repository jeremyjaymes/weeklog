# Output

`output.dir`, `output.mode`, and `output.template` are config keys. Their table is in [CONFIG.md](CONFIG.md). This page is the `weeklog week` command: the recap, output modes, `--save`, `--image`, and interactive `-i`.

`weeklog week` renders the previous full week. `--week`, `--since`, and `--until` select another window, using the same rules as `weeklog collect`.

On a terminal, the command prints a boxed recap: a bar for each day, a colored lane for each source, per-actor counts, the session chain, and the top repositories and pull requests. The chain reads like `git log --graph`. Each row is an AI session (Claude, Codex, or Cursor), a commit it produced, and the pull request or issue the linker attached. The stored reason is dimmed under the row. A hard link uses `*` and `|`. A soft link uses `o` and `:`. A chain that does not fit collapses to a `+N more` row, and so does a week with more chains than the panel can show. `--no-graph` hides the chain. The same panel is drawn without color in the plain recap. A pipe, `NO_COLOR`, or `--plain` prints the Markdown report instead. That Markdown is the same text `--md` writes, so a pipe keeps the previous format.

Under the summary, an ISO week adds one line comparing PRs merged, repositories touched, and agent sessions with the previous ISO week. Those counts are read from stored events. When the previous week has no stored events, the line says there is nothing to compare. The same line is in the Markdown report, so `--plain` and a saved file show it. A window that is not an ISO week omits the line.

`weeklog week --image recap.png` (or `.svg`) writes that styled recap as an image. The image needs the [`freeze`](https://github.com/charmbracelet/freeze) command on `PATH`. `weeklog week --image FILE` writes that styled recap as a PNG or an SVG for sharing. `FILE` must end in `.png` or `.svg`, and an existing file is overwritten. The image uses the default recap width. It stays colored when the text report is Markdown, including a pipe, `NO_COLOR`, `--plain`, and `--md`. `--no-graph` hides the session chain in the image as well. `-i` cannot be combined with `--image`. The text report is still printed or saved.

weeklog renders the image with the [`freeze`](https://github.com/charmbracelet/freeze) command. freeze keeps its screenshot renderer in the program itself, so weeklog runs that binary instead of importing a library. Install `freeze`, then export the week:

```bash
go install github.com/charmbracelet/freeze@latest
# or: brew install charmbracelet/tap/freeze
weeklog week --image ~/journal/weeklog/2026-W41.png
weeklog week --week 2026-W41 --no-graph --image recap.svg
```

When `freeze` is not on `PATH`, weeklog stops before writing the report and prints those install commands.

`-i` opens a scrollable timeline instead of printing or writing the report. The week is one row per day. Enter opens that day's items, and enter again opens the commits, pull requests, and sessions the linker attached. The stored reason is dimmed, with `*` for a hard link and `o` for a soft link, using the same colors as the recap. `q`, ctrl+c, or esc from the day list quits and restores the terminal. `-i` ignores `output.mode` and cannot be combined with `--md`, `--plain`, `--json`, `--out`, `--save`, or `--image`.

When `output.dir` is set and stdout is a terminal, the default `output.mode` of `both` also writes `<output.dir>/<year>-W<week>.md` and prints a dim line naming that file. When `--out` is omitted, `output.mode` `both` writes `<output.dir>/<week>.md` and still prints that recap, then a dim line such as `saved ~/…/2026-W42.md`. A pipe writes the file and prints where it was written. `file` writes the file and does not print the recap. `terminal` prints the report and does not write a file. `--out FILE` and `--out -` ignore `output.mode`. `weeklog week --out -` prints the report to standard output: a boxed recap on a terminal, and Markdown when the output is a pipe, when `NO_COLOR` is set, or when `--plain` is set. Without `output.dir`, the report is printed to the terminal.

`--save` writes the weekly file in addition to that mode. When `output.dir` is set, the file is still `<output.dir>/<YYYY-Www>.md`. When `output.dir` is unset, `--save` writes `~/.weeklog/weeks/<YYYY-Www>.md`. `YYYY-Www` is the ISO week, such as `2026-W41.md`. `--save` with `output.mode` `terminal` writes the file and still prints the recap on a terminal. `--save` cannot be combined with `--out` or `-i`.

```bash
weeklog week
weeklog week --week 2026-W41
weeklog week -i
weeklog week -i --week 2026-W41
weeklog week --save
weeklog week --week 2026-W41 --save
weeklog week --no-graph
weeklog week --json
weeklog week --image recap.png
weeklog week --week 2026-W41 --image recap.svg
weeklog week --out ~/journal/weeklog/2026-W41.md
```

The report groups linked work into Shipped, In progress, Explored, and Abandoned / stale. Each item is tagged with the actor from [Attribution](ATTRIBUTION.md). The summary line under the heading counts items per actor. Commits and sessions that are not linked yet are still listed. Reviews, comments, issues, and unknown events are counted under Other activity.

Time is the sum of agent-session durations and is labeled as an estimate. An item with no session uses 15 minutes for each cluster of commits. The Markdown file starts with `week`, `generated_at`, and `weeklog_version` front matter.

`--md` is the default when the report is saved or piped. `--json` writes the same structure for other tools. `--out FILE` writes that file; `-` writes to standard output. Without `--out`, `output.mode` and `output.dir` choose the destination. `both` writes `<week>.md` (or `.json`) and prints the recap on a terminal. `file` writes only. `terminal` prints only. When `output.dir` is unset, the report is printed unless `--save` is set. `--save` then writes `~/.weeklog/weeks/<week>.md`, or `<week>.json` with `--json`.

Overwrite policy: every write replaces the weekly file. An existing file is not skipped. Text between `<!-- weeklog:notes -->` markers is kept. `output.template` replaces the built-in `text/template`. A relative template path is resolved from the config file's directory.
