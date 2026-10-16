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

func TestWriteInitConfigLiteralsOneFile(t *testing.T) {
	home := isolateInitHome(t)
	path := filepath.Join(home, "xdg", "weeklog", "config.toml")
	const (
		github = "wksec_GH_INIT_aa11"
		linear = `wksec_LIN_INIT_"bb22\`
		cursor = "wksec_CURSOR_INIT_cc33"
	)
	result, err := WriteInitConfig(path, InitChoices{
		Email:        "dev@example.com",
		GitHub:       "octocat",
		Timezone:     "America/Chicago",
		OutputDir:    "~/notes/week",
		OutputMode:   "file",
		Git:          true,
		Claude:       true,
		GitHubSource: true,
		GitHubToken:  github,
		Linear:       true,
		LinearToken:  linear,
		CursorCloud:  true,
		CursorToken:  cursor,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Wrote || result.Existed {
		t.Fatalf("wrote=%v existed=%v", result.Wrote, result.Existed)
	}
	report := result.Report()
	for _, secret := range []string{github, linear, cursor} {
		if strings.Contains(report, secret) || strings.Contains(result.Body, secret) {
			t.Fatalf("report contains a token\n%s", hide(report, github, linear, cursor))
		}
	}
	if strings.Count(result.Body, `token = "[redacted]"`) != 3 {
		t.Fatalf("body = %s", result.Body)
	}
	if strings.Contains(result.Body, "# token = \"\"") || strings.Contains(result.Body, "env:") {
		t.Fatalf("body = %s", result.Body)
	}
	if !strings.Contains(result.Body, `mode = "file"`) || !strings.Contains(result.Body, `dir = "~/notes/week"`) {
		t.Fatalf("body = %s", result.Body)
	}
	if !strings.Contains(result.Body, `type = "cursor-cloud"`) || !strings.Contains(result.Body, `type = "linear"`) {
		t.Fatalf("body = %s", result.Body)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("config dir = %v", names)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{github, cursor} {
		if !strings.Contains(text, secret) {
			t.Fatalf("file missing token\n%s", hide(text, github, linear, cursor))
		}
	}
	if !strings.Contains(text, `wksec_LIN_INIT_\"bb22\\`) {
		t.Fatalf("linear token was not escaped\n%s", hide(text, github, linear, cursor))
	}
	if strings.Contains(text, ".env") || strings.Contains(text, "env:") {
		t.Fatalf("file = %s", hide(text, github, linear, cursor))
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Output.Mode != "file" || cfg.Output.Dir != filepath.Join(home, "notes", "week") {
		t.Fatalf("output = %+v", cfg.Output)
	}
	wantTypes := []string{"git", "claude-code", "github", "linear", "cursor-cloud"}
	if len(cfg.Sources) != len(wantTypes) {
		t.Fatalf("sources = %+v", cfg.Sources)
	}
	for i, typ := range wantTypes {
		if cfg.Sources[i].Type != typ {
			t.Fatalf("source[%d] = %s", i, cfg.Sources[i].Type)
		}
	}
	check, err := Check(context.Background(), path)
	if err != nil || !check.OK() {
		t.Fatalf("check: %v\n%s", err, hide(check.Report(), github, linear, cursor))
	}
	if strings.Contains(check.Report(), github) || strings.Contains(check.Report(), linear) || strings.Contains(check.Report(), cursor) {
		t.Fatal(hide(check.Report(), github, linear, cursor))
	}
	got, ok := check.Secret(4, "token")
	if !ok || got != cursor {
		t.Fatal("cursor token was not stored as a literal")
	}
	got, ok = check.Secret(3, "token")
	if !ok || got != linear {
		t.Fatal("linear token was not stored as a literal")
	}
}

func TestWriteInitConfigKeepsExistingWithoutReplace(t *testing.T) {
	home := isolateInitHome(t)
	path := filepath.Join(home, "config.toml")
	const (
		old    = "wksec_OLD_CONFIG_aa11"
		cursor = "wksec_CURSOR_INIT_dd44"
	)
	original := "token = \"" + old + "\"\n# keep-me\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := WriteInitConfig(path, InitChoices{
		CursorCloud: true,
		CursorToken: cursor,
		OutputMode:  "terminal",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Wrote || !result.Existed {
		t.Fatalf("wrote=%v existed=%v", result.Wrote, result.Existed)
	}
	report := result.Report()
	if strings.Contains(report, old) || strings.Contains(report, cursor) {
		t.Fatal(hide(report, old, cursor))
	}
	if !strings.Contains(report, "not overwritten") || !strings.Contains(report, `type = "cursor-cloud"`) {
		t.Fatal(report)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("file = %q", got)
	}

	result, err = WriteInitConfig(path, InitChoices{
		CursorCloud: true,
		CursorToken: cursor,
		OutputMode:  "terminal",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Wrote || !result.Existed {
		t.Fatalf("wrote=%v existed=%v", result.Wrote, result.Existed)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), old) || !strings.Contains(string(got), cursor) || !strings.Contains(string(got), `mode = "terminal"`) {
		t.Fatalf("file = %s", hide(string(got), cursor))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(home, ".env")); !os.IsNotExist(err) {
		t.Fatalf(".env exists: %v", err)
	}
}

func TestWriteInitConfigRejectsBadTokens(t *testing.T) {
	home := isolateInitHome(t)
	path := filepath.Join(home, "config.toml")
	cases := []InitChoices{
		{CursorCloud: true},
		{Linear: true, LinearToken: "   "},
		{CursorCloud: true, CursorToken: "env:CURSOR_TOKEN"},
		{GitHubSource: true, GitHubToken: "cmd:gh auth token"},
		{GitHubSource: true, GitHubToken: "file:~/token"},
		{CursorCloud: true, CursorToken: "wksec\nSECRET"},
		{OutputMode: "printer"},
	}
	for _, choices := range cases {
		if _, err := WriteInitConfig(path, choices, false); err == nil {
			t.Fatalf("accepted %#v", choices)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("file created for %#v: %v", choices, err)
		}
	}

	result, err := WriteInitConfig(path, InitChoices{GitHubSource: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Body, `token = "cmd:gh auth token"`) || !strings.Contains(result.Body, "# token = \"\"") {
		t.Fatalf("body = %s", result.Body)
	}
	if strings.Contains(result.Body, "\ntype = \"cursor-cloud\"\n") || strings.Contains(result.Body, "\ntype = \"linear\"\n") {
		t.Fatalf("body = %s", result.Body)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `token = "cmd:gh auth token"`) {
		t.Fatalf("file = %s", raw)
	}
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
