package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigCheckDoesNotPrintSecrets(t *testing.T) {
	const (
		resolved = "wksec_RESOLVED_7f3a9c1e"
		leaked   = "wksec_STDERR_c0ffee99"
		literal  = "wksec_LITERAL_bb12dd34"
		cursor   = "wksec_CURSOR_cc44ee01"
	)
	t.Setenv("WEEKLOG_TEST_SECRET", resolved)
	t.Setenv("WEEKLOG_TEST_STDERR", leaked)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte(resolved+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := `
[identity]
timezone = "UTC"
week_starts = "monday"
github = "jeremyjaymes"

[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "github"
token = "env:WEEKLOG_TEST_SECRET"

[[source]]
type = "linear"
token = "file:secret.txt"
ticket_pattern = "GEN-\\d+"

[[source]]
type = "basecamp"
account_id = "123"
token = "cmd:printenv WEEKLOG_TEST_SECRET"

[[source]]
type = "other"
token = "cmd:sh -c 'printf %s \"$WEEKLOG_TEST_STDERR\" >&2; exit 9'"
api_key = "` + literal + `"

[[source]]
type = "cursor-cloud"
token = "` + cursor + `"
`
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runWeeklog(t, "config", "check", "--config", path)
	if err == nil {
		t.Fatal("expected config check to fail")
	}
	combined := stdout + "\n" + stderr
	for _, secret := range []string{resolved, leaked, literal, cursor} {
		if strings.Contains(combined, secret) {
			scrubbed := strings.ReplaceAll(combined, secret, "[canary]")
			t.Fatalf("command output contains secret value\n%s", scrubbed)
		}
	}
	for _, want := range []string{
		"resolved (env:WEEKLOG_TEST_SECRET)",
		"resolved (file:secret.txt)",
		"resolved (cmd:printenv WEEKLOG_TEST_SECRET)",
		"command exited 9",
		"api_key: literal",
		"token: literal",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "literal secret") {
		t.Fatalf("literal secret was warned\n%s", stdout)
	}
	if !strings.Contains(stderr, "config check failed") {
		t.Fatalf("stderr = %q", stderr)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatalf("stderr dumped usage:\n%s", stderr)
	}
}

func TestConfigCheckWarnsWhenGroupOrWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("config mode bits are not meaningful on Windows")
	}
	const literal = "wksec_LITERAL_bb12dd34"
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "` + literal + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runWeeklog(t, "config", "check", "--config", path)
	if err != nil {
		t.Fatalf("loose mode should warn, not fail: %v\n%s\n%s", err, stdout, stderr)
	}
	combined := stdout + "\n" + stderr
	if strings.Contains(combined, literal) {
		t.Fatalf("command output contains secret value\n%s", strings.ReplaceAll(combined, literal, "[canary]"))
	}
	if strings.Contains(stdout, "literal secret") {
		t.Fatalf("literal secret was warned\n%s", stdout)
	}
	for _, want := range []string{
		"token: literal",
		"warning: config: file is group- or world-readable",
		"1 warning",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout)
		}
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestConfigCheckSuccessAndPathOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	xdg := filepath.Join(home, "xdg")
	configDir := filepath.Join(xdg, "weeklog")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)
	body := `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "claude-code"
`
	defaultPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(defaultPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runWeeklog(t, "config", "check")
	if err != nil {
		t.Fatalf("default path: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "config: "+defaultPath) || !strings.Contains(stdout, "\nok\n") {
		t.Fatalf("stdout = %s", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}

	other := filepath.Join(home, "other.toml")
	if err := os.WriteFile(other, []byte(`
[identity]
timezone = "Not/AZone"
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "codex"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runWeeklog(t, "--config", other, "config", "check")
	if err == nil {
		t.Fatal("expected invalid timezone to fail")
	}
	if !strings.Contains(stdout, "config: "+other) || !strings.Contains(stdout, "invalid timezone") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stderr, "config check failed") {
		t.Fatalf("stderr = %q", stderr)
	}
	if strings.Contains(stdout, defaultPath) {
		t.Fatal("flag did not override XDG path")
	}

	stdout, stderr, err = runWeeklog(t, "--config", filepath.Join(home, "missing.toml"), "version")
	if err != nil {
		t.Fatalf("version should ignore --config: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "weeklog version") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestConfigCheckHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "config", "check", "--help")
	if err != nil {
		t.Fatalf("help: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "never prints secret values") {
		t.Fatalf("help = %s", stdout)
	}
}

func TestConfigWithoutSubcommandShowsHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "config")
	if err != nil {
		t.Fatalf("config: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "check") || !strings.Contains(stdout, "Validate config") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func runWeeklog(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}
