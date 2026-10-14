package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/collect"
	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestCollectHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "collect", "--help")
	if err != nil {
		t.Fatalf("help: %v\n%s", err, stderr)
	}
	for _, want := range []string{"--week", "--since", "--until", "--source", "--db", "--timeout", "previous full week"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("help missing %q\n%s", want, stdout)
		}
	}
}

func TestCollectStubsAreSkipped(t *testing.T) {
	home := isolatePlugins(t)
	path := filepath.Join(home, "config.toml")
	body := `
[identity]
timezone = "UTC"
week_starts = "monday"

[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "codex"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runWeeklog(t, "--config", path, "collect", "--week", "2026-W41")
	if err != nil {
		t.Fatalf("collect: %v\n%s\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if !strings.Contains(stdout, "codex: skipped (collector is not implemented)") {
		t.Fatalf("stdout missing skipped codex\n%s", stdout)
	}
	if strings.Contains(stdout, "linear:") {
		t.Fatalf("stdout mentioned linear\n%s", stdout)
	}
	if !strings.Contains(stdout, "window: 2026-W41 (2026-10-05T00:00:00Z .. 2026-10-12T00:00:00Z)") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stdout, "link: ok") || !strings.Contains(stdout, "0 events, 1 source skipped") {
		t.Fatalf("stdout = %s", stdout)
	}
	db := filepath.Join(home, "data", "weeklog", "weeklog.db")
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("default database: %v", err)
	}
}

func TestCollectPartialSources(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	home := isolatePlugins(t)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(gitBin, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	const (
		secret = "weeklog-test-secret"
		token  = "ghp_" + "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
		linear = "weeklog linear token"
	)
	t.Setenv("WEEKLOG_TEST_SECRET", secret)

	repo := filepath.Join(home, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "note.txt"), []byte("note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "note.txt")
	runGit(t, repo, "-c", "user.name=Alice", "-c", "user.email=alice@example.com",
		"commit", "--date=2026-10-08T15:00:00Z", "-m", "Ship "+secret+" "+token)

	plug := `#!/bin/sh
if [ "$1" = "describe" ]; then
  printf '%s\n' '{"protocol":1,"name":"plug","version":"0.1.0","config":{"token":{"type":"secret"}}}'
  exit 0
fi
if [ "$1" != "collect" ]; then
  exit 0
fi
# Read stdin in the shell. The test PATH only contains the plugin dir, which
# does not have cat.
req=
while IFS= read -r line || [ -n "$line" ]; do
  req="${req}${line}
"
done
printf '%s' "$req" > "$HOME/plug-request.json"
case "$req" in
  *'"cursor":"v1"'*)
    printf '%s\n' '{"type":"cursor","cursor":"v1"}'
    ;;
  *)
    printf '%s\n' '{"type":"event","event":{"source_id":"plug-1","kind":"comment","ts":"2026-10-08T12:00:00Z","title":"plugin weeklog-test-secret"}}'
    printf '%s\n' '{"type":"cursor","cursor":"v1"}'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "weeklog-source-plug"), []byte(plug), 0o755); err != nil {
		t.Fatal(err)
	}
	boom := `#!/bin/sh
if [ "$1" = "describe" ]; then
  printf '%s\n' '{"protocol":1,"name":"boom","version":"0.1.0","config":{"token":{"type":"secret"}}}'
  exit 0
fi
printf '%s\n' 'failed weeklog-test-secret' >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "weeklog-source-boom"), []byte(boom), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := filepath.Join(home, "config.toml")
	body := `
[identity]
emails = ["alice@example.com"]
github = "alice"
timezone = "UTC"
week_starts = "monday"

[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "git"
roots = ["` + repo + `"]
authors = ["alice@example.com"]

[[source]]
type = "claude-code"

[[source]]
type = "codex"

[[source]]
type = "github"

[[source]]
type = "linear"
token = "weeklog linear token"

[[source]]
type = "plug"
token = "env:WEEKLOG_TEST_SECRET"

[[source]]
type = "boom"
token = "env:WEEKLOG_TEST_SECRET"
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(home, "weeklog.db")
	args := []string{"--config", cfg, "collect", "--db", db, "--since", "2026-10-05T00:00:00Z", "--until", "2026-10-12T00:00:00Z"}
	stdout, stderr, err := runWeeklog(t, args...)
	if !errors.Is(err, collect.ErrFailed) {
		t.Fatalf("collect: %v\n%s\n%s", err, stdout, stderr)
	}
	for _, want := range []string{
		"source[0] git: ok (1 event)",
		"source[1] claude-code: ok (0 events)",
		"source[2] codex: skipped (collector is not implemented)",
		"source[3] github: failed",
		"gh auth token is unavailable",
		"source[4] linear: failed (linear: collect: token contains whitespace or control characters)",
		"source[5] plug: ok (1 event)",
		"source[6] boom: failed",
		"link: ok",
		"2 events, 3 sources failed, 1 source skipped",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s\nstderr=%s", want, stdout, stderr)
		}
	}
	if strings.Contains(stdout+stderr, secret) || strings.Contains(stdout+stderr, token) || strings.Contains(stdout+stderr, linear) {
		t.Fatal("collect printed a secret")
	}
	assertDBRedacted(t, db, secret, token)

	stdout, stderr, err = runWeeklog(t, args...)
	if !errors.Is(err, collect.ErrFailed) {
		t.Fatalf("second collect: %v\n%s\n%s", err, stdout, stderr)
	}
	req, err := os.ReadFile(filepath.Join(home, "plug-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(req), `"cursor":"v1"`) {
		t.Fatalf("second request = %s", req)
	}
	st := openDB(t, db)
	n, err := st.CountEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("events after second collect = %d", n)
	}

	stdout, stderr, err = runWeeklog(t, append(args, "--source", "git")...)
	if err != nil {
		t.Fatalf("git only: %v\n%s\n%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "boom") || strings.Contains(stdout, "codex") || !strings.Contains(stdout, "source[0] git: ok (1 event)") {
		t.Fatalf("git only stdout = %s", stdout)
	}
}

func TestCollectRejectsBadWindowsWithoutWriting(t *testing.T) {
	home := isolatePlugins(t)
	cfg := filepath.Join(home, "config.toml")
	body := `
[identity]
timezone = "UTC"

[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "codex"
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(home, "missing.db")
	_, stderr, err := runWeeklog(t, "--config", cfg, "collect", "--db", db, "--since", "2026-10-05")
	if err == nil || !strings.Contains(err.Error(), "--since and --until must be set together") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	if _, statErr := os.Stat(db); !os.IsNotExist(statErr) {
		t.Fatal("invalid window created the database")
	}
	_, _, err = runWeeklog(t, "--config", cfg, "collect", "--db", db, "--source", "git")
	if err == nil || !strings.Contains(err.Error(), "source git is not configured") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(db); !os.IsNotExist(statErr) {
		t.Fatal("unknown source created the database")
	}
}

func assertDBRedacted(t *testing.T, path, secret, token string) {
	t.Helper()
	st := openDB(t, path)
	ctx := context.Background()
	recs, err := st.ListEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var commits, plugins int
	for _, rec := range recs {
		blob := rec.Title + rec.Summary
		if strings.Contains(blob, secret) || strings.Contains(blob, token) {
			t.Fatalf("stored event contains a secret: %#v", rec.Event)
		}
		switch rec.Kind {
		case event.KindCommit:
			commits++
			if !strings.Contains(rec.Title, "[REDACTED:config-secret]") || !strings.Contains(rec.Title, "[REDACTED:github-token]") {
				t.Fatalf("commit title = %q", rec.Title)
			}
		case event.KindComment:
			plugins++
			if !strings.Contains(rec.Title, "[REDACTED:config-secret]") {
				t.Fatalf("plugin title = %q", rec.Title)
			}
		}
	}
	if commits != 1 || plugins != 1 {
		t.Fatalf("commits=%d plugins=%d", commits, plugins)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		raw, err := os.ReadFile(path + suffix)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		if strings.Contains(string(raw), secret) || strings.Contains(string(raw), token) {
			t.Fatalf("database file %s contains a secret", suffix)
		}
	}
}

func openDB(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Alice",
		"GIT_AUTHOR_EMAIL=alice@example.com",
		"GIT_AUTHOR_DATE=2026-10-08T15:00:00Z",
		"GIT_COMMITTER_NAME=Alice",
		"GIT_COMMITTER_EMAIL=alice@example.com",
		"GIT_COMMITTER_DATE=2026-10-08T15:00:00Z",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
