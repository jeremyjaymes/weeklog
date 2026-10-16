package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/report"
	"github.com/jeremyjaymes/weeklog/internal/store"
	"github.com/jeremyjaymes/weeklog/internal/timeutil"
)

func TestWeekHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "week", "--help")
	if err != nil {
		t.Fatalf("help: %v\n%s", err, stderr)
	}
	for _, want := range []string{"--week", "--since", "--until", "--md", "--plain", "--no-graph", "--interactive", "--json", "--out", "--save", "--db", "previous full week", "weeklog:notes", "15 minutes", "NO_COLOR", "output.mode", "both", "session chain", "actor_evidence", "cursor/", "scrollable timeline", "Overwrite policy", "~/.weeklog/weeks"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help missing %q\n%s", want, stdout)
		}
	}
}

func TestWeekInteractiveNeedsTerminal(t *testing.T) {
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfig(t, home, dir)
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "-i")
	if err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	if stdout != "" || strings.Contains(stderr, "# Week") {
		t.Fatalf("interactive wrote a report\nstdout=%q\nstderr=%s", stdout, stderr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "2026-W41.md")); !os.IsNotExist(statErr) {
		t.Fatalf("interactive created the markdown file: %v", statErr)
	}

	for _, args := range [][]string{
		{"--json"},
		{"--md"},
		{"--plain"},
		{"--out", filepath.Join(home, "out.md")},
		{"--save"},
	} {
		cmdArgs := append([]string{"--config", cfgPath, "week", "-i"}, args...)
		_, stderr, err = runWeeklog(t, cmdArgs...)
		if err == nil || !strings.Contains(err.Error(), "pass either --interactive or") {
			t.Fatalf("args %v err = %v\n%s", args, err, stderr)
		}
	}
}

func TestWeekMarkdownAndJSON(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if !strings.Contains(stdout, "# Week 2026-W41 (Oct 5 – Oct 11)") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stdout, "Add the gate") || strings.Contains(stdout, "Too early") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stdout, report.NotesMarker) {
		t.Fatalf("stdout missing notes markers\n%s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("json: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `"week": "2026-W41"`) || !strings.Contains(stdout, `"commits": 1`) {
		t.Fatalf("json = %s", stdout)
	}
	if !strings.Contains(stdout, `"actor": "human"`) || !strings.Contains(stdout, `"actor_evidence": "human"`) {
		t.Fatalf("json missing actor\n%s", stdout)
	}

	_, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--md", "--json")
	if err == nil || !strings.Contains(err.Error(), "pass either --md or --json") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	_, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--plain", "--json")
	if err == nil || !strings.Contains(err.Error(), "pass either --plain or --json") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
}

func TestWeekPlainMatchesMarkdown(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	plain, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--plain")
	if err != nil {
		t.Fatalf("plain: %v\n%s\n%s", err, plain, stderr)
	}
	if plain != stdout {
		t.Fatalf("plain stdout differs from the default pipe\n--- plain ---\n%s\n--- default ---\n%s", plain, stdout)
	}
	if !strings.Contains(plain, "# Week 2026-W41 (Oct 5 – Oct 11)") || strings.Contains(plain, "\x1b") {
		t.Fatalf("plain = %s", plain)
	}

	t.Setenv("NO_COLOR", "1")
	noColor, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("no color: %v\n%s", err, stderr)
	}
	if noColor != stdout || strings.Contains(noColor, "\x1b") {
		t.Fatalf("NO_COLOR stdout = %s", noColor)
	}
}

func TestStyledRecapDecision(t *testing.T) {
	cases := []struct {
		name     string
		plain    bool
		md       bool
		template string
		noColor  string
		term     bool
		want     bool
	}{
		{name: "terminal", term: true, want: true},
		{name: "pipe", term: false, want: false},
		{name: "plain flag", term: true, plain: true, want: false},
		{name: "md flag", term: true, md: true, want: false},
		{name: "no color", term: true, noColor: "1", want: false},
		{name: "empty no color", term: true, noColor: "", want: true},
		{name: "template", term: true, template: "custom.tmpl", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wantStyledRecap(tc.plain, tc.md, tc.template, tc.noColor, tc.term)
			if got != tc.want {
				t.Fatalf("styled = %v, want %v", got, tc.want)
			}
		})
	}
	if isTerminal(bytes.NewBuffer(nil)) {
		t.Fatal("a buffer was treated as a terminal")
	}
}

func TestWeekWritesFileAndKeepsNotes(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	out := filepath.Join(home, "journal", "2026-W41.md")

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--out", out)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	if stdout != "wrote "+out+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(body), report.NotesMarker+"\n"+report.NotesMarker, report.NotesMarker+"\nshipped the gate\n"+report.NotesMarker, 1)
	if err := os.WriteFile(out, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--out", out); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(second), "shipped the gate") {
		t.Fatalf("notes lost\n%s", second)
	}
}

func TestWeekOutputDirAndStdoutOverride(t *testing.T) {
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfig(t, home, dir)
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	dest := filepath.Join(dir, "2026-W41.md")
	if stdout != "wrote "+dest+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--out", "-")
	if err != nil {
		t.Fatalf("stdout override: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "# Week 2026-W41") {
		t.Fatalf("stdout = %s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("json file: %v\n%s", err, stderr)
	}
	jsonPath := filepath.Join(dir, "2026-W41.json")
	if stdout != "wrote "+jsonPath+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	body, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"week": "2026-W41"`) {
		t.Fatalf("json = %s", body)
	}
}

func TestWeekCustomTemplateAndRange(t *testing.T) {
	home := isolatePlugins(t)
	if err := os.WriteFile(filepath.Join(home, "custom.tmpl"), []byte("RANGE {{.Range}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := `
[identity]
timezone = "UTC"
week_starts = "monday"

[output]
template = "custom.tmpl"
`
	cfgPath := filepath.Join(home, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--since", "2026-10-05", "--until", "2026-10-12", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "RANGE Oct 5 – Oct 11\n") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestWeekDefaultsToPreviousISOWeek(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	window := timeutil.PreviousWeek(time.Now().UTC(), time.Monday)
	year, week := window.Since.ISOWeek()
	label := fmt.Sprintf("%04d-W%02d", year, week)
	if !strings.Contains(stdout, "# Week "+label+" (") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestWeekRejectsBadWindow(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	_, _, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W99")
	if err == nil {
		t.Fatal("expected an invalid week to fail")
	}
	_, _, err = runWeeklog(t, "--config", cfgPath, "week", "--since", "2026-10-05")
	if err == nil || !strings.Contains(err.Error(), "--since and --until") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanWeekOutput(t *testing.T) {
	const (
		dir  = "/journal"
		base = "2026-W42"
		ext  = ".md"
	)
	file := filepath.Join(dir, base+ext)
	cases := []struct {
		name   string
		out    string
		dir    string
		mode   string
		ext    string
		term   bool
		asJSON bool
		want   weekPlan
	}{
		{name: "out file", out: "/tmp/one.md", dir: dir, mode: config.OutputModeTerminal, term: true, want: weekPlan{path: "/tmp/one.md", status: statusWrote}},
		{name: "out dash", out: "-", dir: dir, mode: config.OutputModeFile, term: true, want: weekPlan{report: true, status: statusNone}},
		{name: "no dir", mode: config.OutputModeFile, term: true, want: weekPlan{report: true, status: statusNone}},
		{name: "file", dir: dir, mode: config.OutputModeFile, term: true, want: weekPlan{path: file, status: statusWrote}},
		{name: "terminal", dir: dir, mode: config.OutputModeTerminal, term: true, want: weekPlan{report: true, status: statusNone}},
		{name: "both tty", dir: dir, mode: config.OutputModeBoth, term: true, want: weekPlan{path: file, report: true, status: statusSaved}},
		{name: "both pipe", dir: dir, mode: config.OutputModeBoth, want: weekPlan{path: file, status: statusWrote}},
		{name: "both json tty", dir: dir, mode: config.OutputModeBoth, ext: ".json", term: true, asJSON: true, want: weekPlan{path: filepath.Join(dir, base+".json"), status: statusWrote}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext := ext
			if tc.ext != "" {
				ext = tc.ext
			}
			got, err := planWeekOutput(tc.out, tc.dir, tc.mode, base, ext, tc.term, tc.asJSON, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("plan = %+v, want %+v", got, tc.want)
			}
		})
	}
	if _, err := planWeekOutput("", dir, "desktop", base, ext, true, false, false); err == nil {
		t.Fatal("expected an invalid mode to fail")
	}
}

func TestPlanWeekSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	const (
		dir  = "/journal"
		base = "2026-W42"
		ext  = ".md"
	)
	file := filepath.Join(dir, base+ext)
	fallback := filepath.Join(home, ".weeklog", "weeks", base+ext)
	cases := []struct {
		name   string
		out    string
		dir    string
		mode   string
		ext    string
		term   bool
		asJSON bool
		save   bool
		want   weekPlan
	}{
		{name: "save both tty", dir: dir, mode: config.OutputModeBoth, term: true, save: true, want: weekPlan{path: file, report: true, status: statusSaved}},
		{name: "save both pipe", dir: dir, mode: config.OutputModeBoth, save: true, want: weekPlan{path: file, status: statusWrote}},
		{name: "save terminal tty", dir: dir, mode: config.OutputModeTerminal, term: true, save: true, want: weekPlan{path: file, report: true, status: statusSaved}},
		{name: "save terminal pipe", dir: dir, mode: config.OutputModeTerminal, save: true, want: weekPlan{path: file, report: true}},
		{name: "save terminal json", dir: dir, mode: config.OutputModeTerminal, term: true, asJSON: true, ext: ".json", save: true, want: weekPlan{path: filepath.Join(dir, base+".json"), status: statusWrote}},
		{name: "save file no dir", mode: config.OutputModeFile, save: true, want: weekPlan{path: fallback, status: statusWrote}},
		{name: "save both no dir pipe", mode: config.OutputModeBoth, save: true, want: weekPlan{path: fallback, status: statusWrote}},
		{name: "save both no dir tty", mode: config.OutputModeBoth, term: true, save: true, want: weekPlan{path: fallback, report: true, status: statusSaved}},
		{name: "no save no dir", mode: config.OutputModeBoth, term: true, want: weekPlan{report: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotExt := ext
			if tc.ext != "" {
				gotExt = tc.ext
			}
			got, err := planWeekOutput(tc.out, tc.dir, tc.mode, base, gotExt, tc.term, tc.asJSON, tc.save)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("plan = %+v, want %+v", got, tc.want)
			}
		})
	}
	if _, err := planWeekOutput("out.md", "", config.OutputModeBoth, base, ext, false, false, true); err == nil || !strings.Contains(err.Error(), "pass either --save or --out") {
		t.Fatalf("err = %v", err)
	}
	if _, err := planWeekOutput("-", dir, config.OutputModeFile, base, ext, true, false, true); err == nil || !strings.Contains(err.Error(), "pass either --save or --out") {
		t.Fatalf("err = %v", err)
	}
}

func TestWeekSaveDefaultDir(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	dest := filepath.Join(home, ".weeklog", "weeks", "2026-W41.md")

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("week without --save wrote %s: %v", dest, err)
	}
	if !strings.Contains(stdout, "# Week 2026-W41 (Oct 5 – Oct 11)") {
		t.Fatalf("stdout = %s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--save")
	if err != nil {
		t.Fatalf("save: %v\n%s\n%s", err, stdout, stderr)
	}
	if stdout != "wrote "+dest+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# Week 2026-W41 (Oct 5 – Oct 11)") || !strings.Contains(string(body), report.NotesMarker) {
		t.Fatalf("file = %s", body)
	}
	if strings.Contains(string(body), "\x1b") {
		t.Fatalf("saved markdown contains ANSI\n%s", body)
	}

	updated := strings.Replace(string(body), report.NotesMarker+"\n"+report.NotesMarker, report.NotesMarker+"\nshipped the gate\n"+report.NotesMarker, 1)
	if err := os.WriteFile(dest, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--save"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(second), "shipped the gate") || !strings.Contains(string(second), "# Week 2026-W41 (Oct 5 – Oct 11)") {
		t.Fatalf("overwrite lost the report or the notes\n%s", second)
	}
}

func TestWeekSaveMatchesPlainReport(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfigMode(t, home, "", "terminal")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	plain, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--plain")
	if err != nil {
		t.Fatalf("plain: %v\n%s", err, stderr)
	}
	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--save")
	if err != nil {
		t.Fatalf("save: %v\n%s\n%s", err, stdout, stderr)
	}
	dest := filepath.Join(home, ".weeklog", "weeks", "2026-W41.md")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != stdout {
		t.Fatalf("file and stdout differ\n--- file ---\n%s\n--- stdout ---\n%s", body, stdout)
	}
	if dropGeneratedAt(stdout) != dropGeneratedAt(plain) {
		t.Fatalf("saved markdown differs from --plain\n--- save ---\n%s\n--- plain ---\n%s", stdout, plain)
	}
}

func dropGeneratedAt(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "generated_at:") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func TestWeekSaveUsesOutputDir(t *testing.T) {
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfigMode(t, home, dir, "terminal")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	dest := filepath.Join(dir, "2026-W41.md")
	fallback := filepath.Join(home, ".weeklog", "weeks", "2026-W41.md")

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--save")
	if err != nil {
		t.Fatalf("save: %v\n%s\n%s", err, stdout, stderr)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != stdout || !strings.Contains(stdout, "# Week 2026-W41 (Oct 5 – Oct 11)") {
		t.Fatalf("stdout = %s\nfile = %s", stdout, body)
	}
	if _, err := os.Stat(fallback); !os.IsNotExist(err) {
		t.Fatalf("fallback file exists: %v", err)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--save", "--json")
	if err != nil {
		t.Fatalf("json: %v\n%s", err, stderr)
	}
	jsonPath := filepath.Join(dir, "2026-W41.json")
	if stdout != "wrote "+jsonPath+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"week": "2026-W41"`) {
		t.Fatalf("json = %s", raw)
	}

	_, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--save", "--out", dest)
	if err == nil || !strings.Contains(err.Error(), "pass either --save or --out") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
}

func TestWeekSaveOnTerminal(t *testing.T) {
	setTerminal(t, true)
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	dest := filepath.Join(home, ".weeklog", "weeks", "2026-W41.md")

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--save")
	if err != nil {
		t.Fatalf("save: %v\n%s\n%s", err, stdout, stderr)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# Week 2026-W41 (Oct 5 – Oct 11)") || strings.Contains(string(body), "\x1b") {
		t.Fatalf("file = %s", body)
	}
	if !strings.Contains(stdout, "╭") || strings.Contains(stdout, "# Week 2026-W41") {
		t.Fatalf("stdout = %q", stdout)
	}
	wantSaved := "saved " + filepath.Join("~", "…", "2026-W41.md")
	if !strings.HasSuffix(stripANSI(stdout), wantSaved+"\n") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSavedLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NO_COLOR", "")
	nested := filepath.Join(home, "journal", "weeklog", "2026-W42.md")
	direct := filepath.Join(home, "2026-W42.md")
	outside := filepath.Join(t.TempDir(), "2026-W42.md")
	collapsed := filepath.Join("~", "…", "2026-W42.md")
	if got := shortenSavedPath(nested); got != collapsed {
		t.Fatalf("nested = %q, want %q", got, collapsed)
	}
	if got := shortenSavedPath(direct); got != filepath.Join("~", "2026-W42.md") {
		t.Fatalf("direct = %q", got)
	}
	if got := shortenSavedPath(outside); got != outside {
		t.Fatalf("outside = %q", got)
	}
	plain := formatSavedLine(nested, false)
	if plain != "saved "+collapsed+"\n" || strings.Contains(plain, "\x1b") {
		t.Fatalf("plain = %q", plain)
	}
	colored := formatSavedLine(nested, true)
	if !strings.Contains(colored, "\x1b[2m") || stripANSI(colored) != plain {
		t.Fatalf("colored = %q", colored)
	}
	if colorSavedLine(false, true) != true || colorSavedLine(true, true) || colorSavedLine(false, false) {
		t.Fatal("colorSavedLine")
	}
	t.Setenv("NO_COLOR", "1")
	if colorSavedLine(false, true) {
		t.Fatal("NO_COLOR enabled color")
	}
}

func TestWeekBothPrintsRecapOnTerminal(t *testing.T) {
	setTerminal(t, true)
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal", "weeklog")
	cfgPath := writeWeekConfigMode(t, home, dir, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	dest := filepath.Join(dir, "2026-W41.md")
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# Week 2026-W41 (Oct 5 – Oct 11)") {
		t.Fatalf("file = %s", body)
	}
	if !strings.Contains(stdout, "╭") || !strings.Contains(stdout, "\x1b") {
		t.Fatalf("stdout = %q", stdout)
	}
	wantSaved := "saved " + filepath.Join("~", "…", "2026-W41.md")
	if !strings.HasSuffix(stripANSI(stdout), wantSaved+"\n") {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.Contains(stdout, "# Week 2026-W41") {
		t.Fatalf("terminal recap included the markdown\n%s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--md")
	if err != nil {
		t.Fatalf("md: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "# Week 2026-W41") || !strings.Contains(stdout, "\x1b[2m") || !strings.HasSuffix(stripANSI(stdout), wantSaved+"\n") {
		t.Fatalf("md stdout = %q", stdout)
	}
}

func TestWeekBothPlainAndNoColorPrintMarkdown(t *testing.T) {
	setTerminal(t, true)
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfigMode(t, home, dir, "both")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	saved := "saved " + filepath.Join("~", "…", "2026-W41.md") + "\n"

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--plain")
	if err != nil {
		t.Fatalf("plain: %v\n%s\n%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "\x1b") || !strings.Contains(stdout, "# Week 2026-W41") || !strings.HasSuffix(stdout, saved) {
		t.Fatalf("plain stdout = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-W41.md")); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NO_COLOR", "1")
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("no color: %v\n%s", err, stderr)
	}
	if strings.Contains(stdout, "\x1b") || !strings.Contains(stdout, "# Week 2026-W41") || !strings.HasSuffix(stdout, saved) {
		t.Fatalf("NO_COLOR stdout = %q", stdout)
	}
}

func TestWeekFileModeSkipsRecap(t *testing.T) {
	setTerminal(t, true)
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfigMode(t, home, dir, "file")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	dest := filepath.Join(dir, "2026-W41.md")
	if stdout != "wrote "+dest+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
}

func TestWeekTerminalModeWritesNoFile(t *testing.T) {
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfigMode(t, home, dir, "terminal")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "# Week 2026-W41") || strings.Contains(stdout, "saved ") {
		t.Fatalf("stdout = %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-W41.md")); !os.IsNotExist(err) {
		t.Fatalf("terminal mode wrote a file: %v", err)
	}

	setTerminal(t, true)
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("tty: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "╭") || strings.Contains(stdout, "saved ") {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-W41.md")); !os.IsNotExist(err) {
		t.Fatalf("terminal mode wrote a file on a tty: %v", err)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("json: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, `"week": "2026-W41"`) || strings.Contains(stdout, "saved ") || strings.Contains(stdout, "wrote ") {
		t.Fatalf("stdout = %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-W41.json")); !os.IsNotExist(err) {
		t.Fatalf("terminal mode wrote json: %v", err)
	}
}

func TestWeekOutFlagsIgnoreMode(t *testing.T) {
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfigMode(t, home, dir, "terminal")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	out := filepath.Join(home, "explicit.md")

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--out", out)
	if err != nil {
		t.Fatalf("out file: %v\n%s\n%s", err, stdout, stderr)
	}
	if stdout != "wrote "+out+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-W41.md")); !os.IsNotExist(err) {
		t.Fatalf("dir file exists: %v", err)
	}

	cfgPath = writeWeekConfigMode(t, home, dir, "file")
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--out", "-")
	if err != nil {
		t.Fatalf("out dash: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "# Week 2026-W41") || strings.Contains(stdout, "wrote ") || strings.Contains(stdout, "saved ") {
		t.Fatalf("stdout = %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-W41.md")); !os.IsNotExist(err) {
		t.Fatalf("--out - wrote a file: %v", err)
	}
}

func TestWeekInvalidMode(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfigMode(t, home, filepath.Join(home, "journal"), "desktop")
	_, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41")
	if err == nil || !strings.Contains(err.Error(), "output.mode") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
}

func TestWeekJSONBothOnTerminalWritesFile(t *testing.T) {
	setTerminal(t, true)
	home := isolatePlugins(t)
	dir := filepath.Join(home, "journal")
	cfgPath := writeWeekConfigMode(t, home, dir, "both")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("json: %v\n%s", err, stderr)
	}
	dest := filepath.Join(dir, "2026-W41.json")
	if stdout != "wrote "+dest+"\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"week": "2026-W41"`) {
		t.Fatalf("json = %s", body)
	}
}

func setTerminal(t *testing.T, term bool) {
	t.Helper()
	prev := detectTerminal
	detectTerminal = func(io.Writer) bool { return term }
	t.Setenv("NO_COLOR", "")
	t.Cleanup(func() { detectTerminal = prev })
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func writeWeekConfig(t *testing.T, home, outputDir string) string {
	t.Helper()
	return writeWeekConfigMode(t, home, outputDir, "")
}

func writeWeekConfigMode(t *testing.T, home, outputDir, mode string) string {
	t.Helper()
	body := `
[identity]
timezone = "UTC"
week_starts = "monday"
`
	if outputDir != "" || mode != "" {
		body += "\n[output]\n"
		if outputDir != "" {
			body += "dir = " + quoteTOML(outputDir) + "\n"
		}
		if mode != "" {
			body += "mode = " + quoteTOML(mode) + "\n"
		}
	}
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func quoteTOML(path string) string {
	return `"` + strings.ReplaceAll(path, `\`, `\\`) + `"`
}

func TestWeekRecapShowsGraph(t *testing.T) {
	setTerminal(t, true)
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedGraphWeekDB(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	plain := stripANSI(stdout)
	for _, want := range []string{"Session chain", "* Claude", "aaaaaaa", "#8", "ref:GEN-1 in prompt", "o Codex", "files overlap: 1"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("recap missing %q\n%s", want, plain)
		}
	}
	if !strings.Contains(stdout, "\x1b[2") {
		t.Fatalf("reason was not dimmed\n%s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--no-graph")
	if err != nil {
		t.Fatalf("no-graph: %v\n%s", err, stderr)
	}
	hidden := stripANSI(stdout)
	if strings.Contains(hidden, "Session chain") || strings.Contains(hidden, "o Codex") || strings.Contains(hidden, "files overlap: 1") {
		t.Fatalf("--no-graph still drew the chain\n%s", hidden)
	}
	if !strings.Contains(stdout, "╭") || !strings.Contains(stripANSI(stdout), "Sources") {
		t.Fatalf("--no-graph dropped the recap\n%s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--plain")
	if err != nil {
		t.Fatalf("plain: %v\n%s", err, stderr)
	}
	if strings.Contains(stdout, "Session chain") || strings.Contains(stdout, "\x1b") || !strings.Contains(stdout, "# Week 2026-W41") {
		t.Fatalf("plain = %s", stdout)
	}
}

func seedGraphWeekDB(t *testing.T, path string) {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	shaA := strings.Repeat("a", 40)
	shaB := strings.Repeat("b", 40)
	res, err := st.UpsertEvents(context.Background(), []event.Event{
		{
			Source: "claude-code", SourceID: "sess-1", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC), Title: "Admin gate",
			Repo: "github.com/acme/app",
		},
		{
			Source: "git", SourceID: shaA, Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 14, 30, 0, 0, time.UTC), Title: "Add the gate",
			Repo: "github.com/acme/app",
		},
		{
			Source: "github", SourceID: "acme/app#8", Kind: event.KindPR,
			TS: time.Date(2026, 10, 8, 16, 0, 0, 0, time.UTC), Title: "Add the gate",
			State: event.StateMerged, URL: "https://github.com/acme/app/pull/8",
			Repo: "github.com/acme/app",
		},
		{
			Source: "codex", SourceID: "roll-1", Kind: event.KindAgentSession,
			TS: time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC), Title: "Spike",
			Repo: "github.com/acme/otto",
		},
		{
			Source: "git", SourceID: shaB, Kind: event.KindCommit,
			TS: time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC), Title: "Try the spike",
			Repo: "github.com/acme/otto",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = st.ReplaceWorkItems(context.Background(), []store.WorkItemDraft{
		{
			Title: "Add the gate",
			Events: []store.WorkItemLink{
				{EventID: res.IDs[0], Reason: "ref:GEN-1 in prompt"},
				{EventID: res.IDs[1], Reason: "ref:GEN-1 in commit message"},
				{EventID: res.IDs[2], Reason: "ref:GEN-1 in title"},
			},
		},
		{
			Title: "Spike",
			Events: []store.WorkItemLink{
				{EventID: res.IDs[3], Reason: "files overlap: 1"},
				{EventID: res.IDs[4], Reason: "files overlap: 1"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedWeekDB(t *testing.T, path string) {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	_, err = st.UpsertEvents(context.Background(), []event.Event{
		{
			Source: "git", SourceID: strings.Repeat("a", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC), Title: "Add the gate",
			Repo: "github.com/acme/app", Branch: "main",
		},
		{
			Source: "git", SourceID: strings.Repeat("b", 40), Kind: event.KindCommit,
			TS: time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC), Title: "Too early",
			Repo: "github.com/acme/app", Branch: "main",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
