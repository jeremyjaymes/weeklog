//go:build unix

package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitGitHubAndGitEmail(t *testing.T) {
	const canary = "wksec_INIT_CANARY_9f3a"
	// Look these up before isolateInitHome replaces PATH. config check runs
	// the token reference with `sh -c`. On macOS, git and sh are in different
	// directories (/usr/bin and /bin, or Homebrew and /bin). A PATH that only
	// contains git's directory cannot start sh, so the check reports
	// "command failed" instead of running the fake gh.
	gitBin := commandDir(t, "git")
	shellBin := commandDir(t, "sh")
	home := isolateInitHome(t)
	bin := filepath.Join(home, "bin")
	writeGH(t, bin, `#!/bin/sh
if [ "$1" = auth ] && [ "$2" = status ]; then
  printf '%s\n' 'github.com' >&2
  printf '%s\n' '  ✓ Logged in to github.com account octocat (keyring)' >&2
  printf '%s\n' '  - Token: `+canary+`' >&2
  exit 0
fi
if [ "$1" = auth ] && [ "$2" = token ]; then
  printf '%s\n' '`+canary+`'
  exit 0
fi
exit 1
`)
	// Resolve /var -> /private/var so the shell looks up gh through the same
	// path the kernel uses for the temp directory.
	bin = evalSymlinks(t, bin)
	t.Setenv("PATH", joinPath(bin, gitBin, shellBin))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\temail = dev@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Projects", "app", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Projects", "other", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside")
	if err := os.MkdirAll(filepath.Join(outside, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "Projects", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, "xdg", "weeklog", "config.toml")
	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, result, "git", true, "2 repositories under ~/Projects")
	assertStatus(t, result, "codex", true, "~/.codex")
	assertStatus(t, result, "github", true, "gh auth, login octocat")
	if strings.Contains(result.Body, canary) || strings.Contains(result.Report(), canary) {
		t.Fatal("init kept the token")
	}
	if !strings.Contains(result.Body, "emails = [\"dev@example.com\"]") || !strings.Contains(result.Body, "authors = [\"dev@example.com\"]") {
		t.Fatalf("body = %s", result.Body)
	}
	if !strings.Contains(result.Body, "github = \"octocat\"") || !strings.Contains(result.Body, "token = \"cmd:gh auth token\"") {
		t.Fatalf("body = %s", result.Body)
	}
	// The merged codex collector accepts no source keys besides type.
	if !strings.Contains(result.Body, "[[source]]\ntype = \"codex\"\n\n[[source]]\ntype = \"github\"\n") {
		t.Fatalf("codex block = %s", result.Body)
	}

	check, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	report := check.Report()
	if !check.OK() || strings.Contains(report, canary) {
		t.Fatalf("check leaked or failed\n%s", strings.ReplaceAll(report, canary, "[canary]"))
	}
	if !strings.Contains(report, "resolved (cmd:gh auth token)") {
		t.Fatal(report)
	}
}

func TestInitSkipsUnauthenticatedGH(t *testing.T) {
	const canary = "wksec_INIT_CANARY_9f3a"
	home := isolateInitHome(t)
	bin := filepath.Join(home, "bin")
	writeGH(t, bin, `#!/bin/sh
if [ "$1" = auth ] && [ "$2" = status ]; then
  printf '%s\n' '  - Token: `+canary+`' >&2
  exit 1
fi
if [ "$1" = auth ] && [ "$2" = token ]; then
  printf '%s\n' '`+canary+`'
  exit 0
fi
exit 1
`)
	path := filepath.Join(home, "config.toml")
	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, result, "github", false, "gh is not authenticated")
	if strings.Contains(result.Body, "\ntype = \"github\"\n") || strings.Contains(result.Report(), canary) {
		t.Fatal(result.Report())
	}
}

func TestInitSkipsGHWhenTokenCommandFails(t *testing.T) {
	home := isolateInitHome(t)
	bin := filepath.Join(home, "bin")
	writeGH(t, bin, `#!/bin/sh
if [ "$1" = auth ] && [ "$2" = status ]; then
  printf '%s\n' 'Logged in to github.com account octocat (keyring)'
  exit 0
fi
exit 1
`)
	path := filepath.Join(home, "config.toml")
	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, result, "github", false, "gh is not authenticated")
	if strings.Contains(result.Body, "octocat") {
		t.Fatalf("login was written without a token command\n%s", result.Body)
	}
}

func writeGH(t *testing.T, dir, script string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func commandDir(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("%s is required", name)
	}
	return filepath.Dir(evalSymlinks(t, path))
}

func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func joinPath(dirs ...string) string {
	seen := make(map[string]struct{}, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		out = append(out, dir)
	}
	return strings.Join(out, string(os.PathListSeparator))
}
