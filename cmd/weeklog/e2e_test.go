//go:build unix

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const (
	e2eWeek        = "2026-W41"
	e2eGitHubToken = "wksec_E2E_GITHUB_9f3a"
	e2eLinearToken = "wksec_E2E_LINEAR_9f3a"
	e2eLinearEnv   = "WEEKLOG_E2E_LINEAR_TOKEN"
	e2eGeneratedAt = "generated_at: 1970-01-01T00:00:00Z"
)

// generatedAtLine is the front-matter clock. The golden file stores a fixed
// value so a later run does not differ only by the time the report was built.
var generatedAtLine = regexp.MustCompile(`(?m)^generated_at: \S+`)

// TestFixtureHomeE2E builds the weeklog binary and runs it the way a user
// would: weeklog init, then weeklog collect, then weeklog week --md.
//
// The child process gets a temporary HOME and XDG directories. Claude Code
// and Codex use the golden session files under testdata. A git repository
// is created under ~/Projects. GitHub and Linear replay testdata over
// httptest. The child's HTTP proxy is a closed port. Go does not proxy
// 127.0.0.1, so the fixture servers still answer and a request to the public
// APIs fails.
//
// init writes git, claude-code, codex, and github. It does not discover
// Linear, and it cannot know the fixture port. After init, the test edits
// the file that init wrote: github endpoint points at the loopback server,
// and a linear source is appended. That is the same edit a user makes when
// adding a source. config check accepts endpoint only for loopback http.
//
// --week 2026-W41 and TZ=UTC keep the window stable. The Markdown file is
// compared with testdata/e2e/2026-W41.md after generated_at is replaced.
// WEEKLOG_UPDATE_GOLDEN=1 rewrites that file. See testdata/e2e/README.md.
func TestFixtureHomeE2E(t *testing.T) {
	bin := buildWeeklog(t)
	home := t.TempDir()
	gitBin := commandDir(t, "git")
	shellBin := commandDir(t, "sh")

	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = Alice\n\temail = alice@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	copyTree(t, testdataPath(t, "claude"), filepath.Join(home, ".claude"))
	copyTree(t, testdataPath(t, "codex"), filepath.Join(home, ".codex"))
	buildE2ERepo(t, home)

	binDir := filepath.Join(home, "bin")
	writeE2EGH(t, binDir)
	binDir = evalSymlinks(t, binDir)

	githubSrv := githubFixtureServer(t)
	linearSrv := linearFixtureServer(t)
	env := e2eEnv(t, home, joinPath(binDir, gitBin, shellBin))

	initOut, initErr, err := runWeeklogBin(t, bin, env, "init")
	if err != nil {
		t.Fatalf("init: %v\n%s\n%s", err, initOut, initErr)
	}
	for _, want := range []string{
		"git: enabled (1 repository under ~/Projects)",
		"claude-code: enabled (~/.claude)",
		"codex: enabled (~/.codex)",
		"github: enabled (gh auth, login alice)",
		"wrote:",
	} {
		if !strings.Contains(initOut, want) {
			t.Fatalf("init missing %q\n%s", want, initOut)
		}
	}
	if strings.Contains(initOut, e2eGitHubToken) || strings.Contains(initErr, e2eGitHubToken) {
		t.Fatal("init printed the github token")
	}

	cfgPath := filepath.Join(home, ".config", "weeklog", "config.toml")
	addFixtureSources(t, cfgPath, githubSrv.URL, linearSrv.URL)

	collectOut, collectErr, err := runWeeklogBin(t, bin, env, "collect", "--week", e2eWeek)
	if err != nil {
		t.Fatalf("collect: %v\n%s\n%s", err, collectOut, collectErr)
	}
	for _, src := range []string{"git", "claude-code", "codex", "github", "linear"} {
		if !strings.Contains(collectOut, "] "+src+": ok (") {
			t.Fatalf("collect missing %s ok\n%s\n%s", src, collectOut, collectErr)
		}
	}
	if strings.Contains(collectOut, ": failed (") || !strings.Contains(collectOut, "link: ok\n") {
		t.Fatalf("collect did not finish\n%s\n%s", collectOut, collectErr)
	}
	assertNoSecrets(t, "collect", collectOut+"\n"+collectErr)

	weekOut, weekErr, err := runWeeklogBin(t, bin, env, "week", "--week", e2eWeek, "--md")
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, weekOut, weekErr)
	}
	reportPath := filepath.Join(home, "journal", "weeklog", e2eWeek+".md")
	if !strings.Contains(weekOut, "wrote "+reportPath+"\n") {
		t.Fatalf("week stdout = %q\n%s", weekOut, weekErr)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	report := string(raw)
	assertNoSecrets(t, "report", report)
	if strings.Contains(report, home) {
		t.Fatalf("report contains the fixture home\n%s", report)
	}
	got := normalizeWeek(report)
	if !strings.Contains(got, e2eGeneratedAt) {
		t.Fatalf("generated_at was not normalized\n%s", got)
	}
	for _, want := range []string{
		"# Week 2026-W41 (Oct 5 – Oct 11)",
		"weeklog_version: dev",
		"Fix Stripe webhook retries",
		"GEN-301",
		"· 2 commits ·",
		"ref:GEN-301 in commit message",
		"Claude Code",
		"Codex",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q\n%s", want, got)
		}
	}

	golden := testdataPath(t, filepath.Join("e2e", e2eWeek+".md"))
	if os.Getenv("WEEKLOG_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v\nregenerate with WEEKLOG_UPDATE_GOLDEN=1", err)
	}
	if got != string(want) {
		t.Fatalf("week report mismatch\n%s\nregenerate with WEEKLOG_UPDATE_GOLDEN=1 and review the diff", unifiedDiff(string(want), got))
	}
}

func buildWeeklog(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "weeklog")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/weeklog")
	cmd.Dir = moduleRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		t.Fatalf("build weeklog: %v\n%s", err, buf.String())
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func buildE2ERepo(t *testing.T, home string) {
	t.Helper()
	repo := filepath.Join(home, "Projects", "otto")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	e2eGit(t, repo, nil, "init", "-b", "cursor/fix-webhooks")
	e2eGit(t, repo, nil, "remote", "add", "origin", "git@github.com:acme/otto.git")
	writeRepoFile(t, filepath.Join(repo, "README.md"), "week\n")
	e2eGit(t, repo, nil, "add", "README.md")
	e2eCommit(t, repo, "2026-10-06T15:00:00Z", "Start GEN-301")
	writeRepoFile(t, filepath.Join(repo, "app", "retry.go"), "package retry\n")
	e2eGit(t, repo, nil, "add", "app/retry.go")
	e2eCommit(t, repo, "2026-10-08T16:00:00Z", "Handle webhook retries\n\nFixes GEN-301")
}

func e2eCommit(t *testing.T, dir, date, message string) {
	t.Helper()
	e2eGit(t, dir, []string{
		"GIT_AUTHOR_NAME=Alice",
		"GIT_AUTHOR_EMAIL=alice@example.com",
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=Alice",
		"GIT_COMMITTER_EMAIL=alice@example.com",
		"GIT_COMMITTER_DATE=" + date,
	}, "commit", "-m", message)
}

func e2eGit(t *testing.T, dir string, extra []string, args ...string) {
	t.Helper()
	base := []string{
		"-c", "user.name=Alice",
		"-c", "user.email=alice@example.com",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
		"-c", "core.autocrlf=false",
	}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = dir
	env := append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	cmd.Env = lastEnv(append(env, extra...))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeE2EGH(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = auth ] && [ \"$2\" = status ]; then\n" +
		"  printf '%s\\n' 'Logged in to github.com account alice (keyring)'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = auth ] && [ \"$2\" = token ]; then\n" +
		"  printf '%s\\n' '" + e2eGitHubToken + "'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func addFixtureSources(t *testing.T, path, githubURL, linearURL string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	const tokenLine = "token = \"cmd:gh auth token\"\n"
	if !strings.Contains(text, tokenLine) {
		t.Fatalf("init config missing github token line\n%s", text)
	}
	if strings.Contains(text, e2eGitHubToken) || strings.Contains(text, e2eLinearToken) {
		t.Fatal("init wrote a token value")
	}
	text = strings.Replace(text, tokenLine, tokenLine+fmt.Sprintf("endpoint = %q\n", githubURL), 1)
	text += fmt.Sprintf("\n[[source]]\ntype = \"linear\"\ntoken = \"env:%s\"\nendpoint = %q\n", e2eLinearEnv, linearURL)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func e2eEnv(t *testing.T, home, path string) []string {
	t.Helper()
	deadProxy := "http://127.0.0.1:9"
	values := []string{
		"PATH=" + path,
		"HOME=" + home,
		"USERPROFILE=" + home,
		"TMPDIR=" + os.TempDir(),
		"TZ=UTC",
		"LANG=C",
		"LC_ALL=C",
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		e2eLinearEnv + "=" + e2eLinearToken,
		"HTTP_PROXY=" + deadProxy,
		"HTTPS_PROXY=" + deadProxy,
		"ALL_PROXY=" + deadProxy,
		"http_proxy=" + deadProxy,
		"https_proxy=" + deadProxy,
		"all_proxy=" + deadProxy,
		"GH_PROMPT_DISABLED=1",
		"GH_NO_UPDATE_NOTIFIER=1",
	}
	return values
}

func runWeeklogBin(t *testing.T, bin string, env []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func normalizeWeek(s string) string {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return generatedAtLine.ReplaceAllString(s, e2eGeneratedAt)
}

func assertNoSecrets(t *testing.T, label, text string) {
	t.Helper()
	for _, secret := range []string{e2eGitHubToken, e2eLinearToken} {
		if strings.Contains(text, secret) {
			t.Fatalf("%s contains %s", label, secret)
		}
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

func unifiedDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")
	var b strings.Builder
	n := len(wantLines)
	if len(gotLines) > n {
		n = len(gotLines)
	}
	shown := 0
	for i := 0; i < n && shown < 80; i++ {
		var left, right string
		if i < len(wantLines) {
			left = wantLines[i]
		}
		if i < len(gotLines) {
			right = gotLines[i]
		}
		if left == right {
			continue
		}
		fmt.Fprintf(&b, "line %d\n- %s\n+ %s\n", i+1, left, right)
		shown++
	}
	if shown == 0 && want != got {
		fmt.Fprintf(&b, "length want %d got %d\n", len(want), len(got))
	}
	return b.String()
}
