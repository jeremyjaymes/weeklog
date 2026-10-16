package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommandWritesAndChecks(t *testing.T) {
	home := isolateInitCommand(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Projects", "app", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runWeeklog(t, "init")
	if err != nil {
		t.Fatalf("init: %v\n%s\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	path := filepath.Join(home, "xdg", "weeklog", "config.toml")
	if !strings.Contains(stdout, "wrote: "+path) || !strings.Contains(stdout, "claude-code: enabled (~/.claude)") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stdout, "git: enabled (1 repository under ~/Projects)") {
		t.Fatalf("stdout = %s", stdout)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatal(stderr)
	}

	stdout, stderr, err = runWeeklog(t, "config", "check")
	if err != nil {
		t.Fatalf("check: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "config: "+path) || !strings.HasSuffix(stdout, "ok\n") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestInitCommandKeepsExistingFile(t *testing.T) {
	home := isolateInitCommand(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "xdg", "weeklog", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const marker = "wksec_OLD_CONFIG_aa11"
	original := "# keep-me\ntoken = \"" + marker + "\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runWeeklog(t, "init")
	if err != nil {
		t.Fatalf("init: %v\n%s\n%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, marker) {
		t.Fatal("existing secret was printed")
	}
	if !strings.Contains(stdout, "not overwritten") || !strings.Contains(stdout, "would write:") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stdout, `type = "claude-code"`) {
		t.Fatalf("stdout = %s", stdout)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("file = %q", got)
	}

	stdout, stderr, err = runWeeklog(t, "init", "--force")
	if err != nil {
		t.Fatalf("force: %v\n%s\n%s", err, stdout, stderr)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), marker) || !strings.Contains(string(got), `type = "claude-code"`) {
		t.Fatalf("file = %s", got)
	}
	stdout, stderr, err = runWeeklog(t, "config", "check")
	if err != nil {
		t.Fatalf("check: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.HasSuffix(stdout, "ok\n") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestInitCommandConfigFlag(t *testing.T) {
	home := isolateInitCommand(t)
	other := filepath.Join(home, "custom", "week.toml")
	stdout, stderr, err := runWeeklog(t, "init", "--config", other)
	if err != nil {
		t.Fatalf("init: %v\n%s\n%s", err, stdout, stderr)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(home, "xdg", "weeklog", "config.toml")
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Fatalf("default path was created: %v", err)
	}
	if !strings.Contains(stdout, "wrote: "+other) {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestInitHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "init", "--help")
	if err != nil {
		t.Fatalf("help: %v\n%s", err, stderr)
	}
	for _, want := range []string{"CODEX_HOME", "~/.claude", "--force", "gh", "output.mode", "0600", "paste your key", "cmd:gh auth token"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help missing %q\n%s", want, stdout)
		}
	}
}

func isolateInitCommand(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("TZ", "UTC")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "no-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return home
}
