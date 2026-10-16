package config

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestInitWritesDetectedSources(t *testing.T) {
	home := isolateInitHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	codex := filepath.Join(home, "codex-home")
	if err := os.MkdirAll(codex, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codex)
	repo := filepath.Join(home, "Projects", "a", "b", "c")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Deeper than the collector scan, and a symlink, must not count.
	if err := os.MkdirAll(filepath.Join(repo, "d", "e", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, "xdg", "weeklog", "config.toml")
	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Wrote {
		t.Fatal("expected a write")
	}
	assertStatus(t, result, "git", true, "1 repository under ~/Projects")
	assertStatus(t, result, "claude-code", true, "~/.claude")
	assertStatus(t, result, "codex", true, "$CODEX_HOME")
	assertStatus(t, result, "github", false, "gh not on PATH")
	if !strings.Contains(result.Body, "timezone = \"America/Chicago\"") {
		t.Fatalf("body = %s", result.Body)
	}
	if strings.Contains(result.Body, "type = \"github\"") {
		t.Fatalf("github was enabled without gh\n%s", result.Body)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Identity.Timezone != "America/Chicago" || cfg.Identity.WeekStarts != "monday" {
		t.Fatalf("identity = %+v", cfg.Identity)
	}
	if cfg.Output.Dir != filepath.Join(home, "journal", "weeklog") {
		t.Fatalf("dir = %q", cfg.Output.Dir)
	}
	if cfg.Output.Mode != "both" || !strings.Contains(result.Body, `mode = "both"`) {
		t.Fatalf("mode = %q\n%s", cfg.Output.Mode, result.Body)
	}
	if !strings.Contains(result.Body, "terminal prints only") {
		t.Fatalf("init did not document output.mode\n%s", result.Body)
	}
	want := []string{"git", "claude-code", "codex"}
	if len(cfg.Sources) != len(want) {
		t.Fatalf("sources = %+v", cfg.Sources)
	}
	for i, typ := range want {
		if cfg.Sources[i].Type != typ {
			t.Fatalf("source[%d] = %s", i, cfg.Sources[i].Type)
		}
	}
	roots, _ := cfg.Sources[0].Settings["roots"].([]string)
	if len(roots) != 1 || roots[0] != filepath.Join(home, "Projects") {
		t.Fatalf("roots = %#v", roots)
	}
	if !strings.Contains(result.Body, "Paste your key into token.") || !strings.Contains(result.Body, "# token = \"\"") {
		t.Fatalf("missing token placeholder\n%s", result.Body)
	}
	if !strings.Contains(result.Body, `type = "cursor-cloud"`) || !strings.Contains(result.Body, "Paste your API key into token.") {
		t.Fatalf("missing cursor-cloud placeholder\n%s", result.Body)
	}
	check, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !check.OK() {
		t.Fatal(check.Report())
	}
	if strings.Contains(check.Report(), "group- or world-readable") {
		t.Fatal(check.Report())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestInitSkipsMissingSources(t *testing.T) {
	home := isolateInitHome(t)
	if err := os.WriteFile(filepath.Join(home, ".claude"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Projects", "a", "b", "c", "d", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "missing-claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex-file"))
	if err := os.WriteFile(filepath.Join(home, "codex-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(home, "xdg", "weeklog", "config.toml")
	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, result, "git", false, "no repositories under ~/Projects")
	assertStatus(t, result, "claude-code", false, "$CLAUDE_CONFIG_DIR not found")
	assertStatus(t, result, "codex", false, "$CODEX_HOME is not a directory")
	if uncommentedSource.MatchString(result.Body) {
		t.Fatalf("sources were written\n%s", result.Body)
	}
	if !strings.Contains(result.Body, "Paste your key into token.") || !strings.Contains(result.Body, "# token = \"\"") {
		t.Fatalf("missing token placeholder\n%s", result.Body)
	}
	if !strings.Contains(result.Body, `type = "cursor-cloud"`) || !strings.Contains(result.Body, "Paste your API key into token.") {
		t.Fatalf("missing cursor-cloud placeholder\n%s", result.Body)
	}
	check, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !check.OK() {
		t.Fatal(check.Report())
	}
	if !strings.Contains(check.Report(), "no sources configured") {
		t.Fatal(check.Report())
	}
}

func TestInitCodexHomeBeatsDefaultDir(t *testing.T) {
	home := isolateInitHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(home, "absent-codex"))

	path := filepath.Join(home, "config.toml")
	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, result, "codex", false, "$CODEX_HOME not found")
	if strings.Contains(result.Body, "type = \"codex\"") {
		t.Fatalf("codex followed ~/.codex while CODEX_HOME was set\n%s", result.Body)
	}
}

func TestInitExpandsTildeConfigDir(t *testing.T) {
	home := isolateInitHome(t)
	if err := os.MkdirAll(filepath.Join(home, "claude-alt"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "~/claude-alt")
	path := filepath.Join(home, "config.toml")
	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	assertStatus(t, result, "claude-code", true, "$CLAUDE_CONFIG_DIR")
}

func TestInitKeepsExistingConfig(t *testing.T) {
	home := isolateInitHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.toml")
	const marker = "wksec_OLD_CONFIG_aa11"
	original := "token = \"" + marker + "\"\n# keep-me\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Init(context.Background(), InitOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if result.Wrote || !result.Existed {
		t.Fatalf("wrote=%v existed=%v", result.Wrote, result.Existed)
	}
	report := result.Report()
	if strings.Contains(report, marker) {
		t.Fatal("report printed the existing config")
	}
	if !strings.Contains(report, "not overwritten") || !strings.Contains(report, "would write:") {
		t.Fatal(report)
	}
	if !strings.Contains(report, "type = \"claude-code\"") {
		t.Fatal(report)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("file = %q", got)
	}

	result, err = Init(context.Background(), InitOptions{Path: path, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Wrote {
		t.Fatal("force did not write")
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), marker) || !strings.Contains(string(got), "type = \"claude-code\"") {
		t.Fatalf("file = %s", got)
	}
	check, err := Check(context.Background(), path)
	if err != nil || !check.OK() {
		t.Fatalf("check: %v\n%s", err, check.Report())
	}
}

func TestInitRejectsDirectoryAndEmptyPath(t *testing.T) {
	home := isolateInitHome(t)
	dir := filepath.Join(home, "config.toml")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(context.Background(), InitOptions{Path: dir}); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatal(err)
	}
	if _, err := Init(context.Background(), InitOptions{}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatal(err)
	}
}

func TestParseGHLoginIgnoresToken(t *testing.T) {
	const token = "wksec_INIT_CANARY_9f3a"
	current := "github.com\n  ✓ Logged in to github.com account octocat (keyring)\n  - Token: " + token + "\n"
	if got := parseGHLogin(current); got != "octocat" {
		t.Fatalf("login = %q", got)
	}
	older := "  ✓ Logged in to github.com as octocat (oauth_token)\n  ✓ Token: " + token + "\n"
	if got := parseGHLogin(older); got != "octocat" {
		t.Fatalf("login = %q", got)
	}
	if parseGHLogin("  - Token: "+token) != "" {
		t.Fatal("token line was parsed as a login")
	}
	if validGitHubLogin(token) {
		t.Fatal("canary was accepted as a login")
	}
}

func TestValidZone(t *testing.T) {
	if validZone("America/Chicago") != "America/Chicago" {
		t.Fatal(validZone("America/Chicago"))
	}
	if validZone("utc") != "UTC" || validZone("local") != "" || validZone("Not/AZone") != "" {
		t.Fatalf("utc=%q local=%q bad=%q", validZone("utc"), validZone("local"), validZone("Not/AZone"))
	}
}

var uncommentedSource = regexp.MustCompile(`(?m)^\[\[source\]\]`)

func assertStatus(t *testing.T, result InitResult, typ string, enabled bool, detail string) {
	t.Helper()
	for _, src := range result.Sources {
		if src.Type != typ {
			continue
		}
		if src.Enabled != enabled || src.Detail != detail {
			t.Fatalf("%s = enabled:%v detail:%q", typ, src.Enabled, src.Detail)
		}
		return
	}
	t.Fatalf("missing status %s", typ)
}

func isolateInitHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("TZ", "America/Chicago")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "no-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return home
}
