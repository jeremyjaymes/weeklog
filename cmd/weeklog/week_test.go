package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	for _, want := range []string{"--week", "--since", "--until", "--md", "--plain", "--json", "--out", "--db", "previous full week", "weeklog:notes", "15 minutes", "NO_COLOR"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help missing %q\n%s", want, stdout)
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

func writeWeekConfig(t *testing.T, home, outputDir string) string {
	t.Helper()
	body := `
[identity]
timezone = "UTC"
week_starts = "monday"
`
	if outputDir != "" {
		body += "\n[output]\ndir = " + quoteTOML(outputDir) + "\n"
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
