package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadPlanExample(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const sample = `
[identity]
emails  = ["jeremy@geneseesoftware.com"]
github  = "jeremyjaymes"
timezone = "America/Chicago"
week_starts = "monday"

[output]
dir = "~/journal/weeklog"            # writes 2026-W41.md
template = ""                         # blank uses the built-in template

[[source]]
type = "git"
roots = ["~/Projects"]                # scanned for repos, depth 3
authors = ["jeremy@geneseesoftware.com"]

[[source]]
type = "claude-code"                  # reads CLAUDE_CONFIG_DIR or ~/.claude

[[source]]
type = "codex"                        # reads CODEX_HOME or ~/.codex

[[source]]
type = "github"
token = "cmd:gh auth token"

[[source]]
type = "linear"
# Paste your key.
token = ""
ticket_pattern = "GEN-\\d+"

[[source]]
type = "cursor-cloud"
# Paste your Cursor API key. A literal value is the normal form.
token = ""

[[source]]                            # an external plugin works the same way
type = "basecamp"                     # runs weeklog-source-basecamp
account_id = "123"
token = "cmd:op read op://dev/basecamp/token"
`
	path := writeConfig(t, sample)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Identity.GitHub != "jeremyjaymes" {
		t.Fatalf("github = %q", cfg.Identity.GitHub)
	}
	if len(cfg.Identity.Emails) != 1 || cfg.Identity.Emails[0] != "jeremy@geneseesoftware.com" {
		t.Fatalf("emails = %#v", cfg.Identity.Emails)
	}
	if cfg.Identity.Timezone != "America/Chicago" {
		t.Fatalf("timezone = %q", cfg.Identity.Timezone)
	}
	if cfg.Identity.WeekStarts != "monday" {
		t.Fatalf("week_starts = %q", cfg.Identity.WeekStarts)
	}
	if cfg.Output.Dir != filepath.Join(home, "journal", "weeklog") {
		t.Fatalf("dir = %q", cfg.Output.Dir)
	}
	if cfg.Output.Template != "" {
		t.Fatalf("template = %q", cfg.Output.Template)
	}
	mode, err := cfg.Output.ResolvedMode()
	if err != nil || mode != OutputModeBoth {
		t.Fatalf("mode = %q, %v", mode, err)
	}
	if len(cfg.Sources) != 7 {
		t.Fatalf("sources = %d", len(cfg.Sources))
	}

	wantTypes := []string{"git", "claude-code", "codex", "github", "linear", "cursor-cloud", "basecamp"}
	for i, want := range wantTypes {
		if cfg.Sources[i].Type != want {
			t.Fatalf("source[%d].type = %q, want %q", i, cfg.Sources[i].Type, want)
		}
	}
	roots, ok := cfg.Sources[0].Settings["roots"].([]string)
	if !ok || len(roots) != 1 || roots[0] != filepath.Join(home, "Projects") {
		t.Fatalf("roots = %#v", cfg.Sources[0].Settings["roots"])
	}
	authors, ok := cfg.Sources[0].Settings["authors"].([]string)
	if !ok || len(authors) != 1 || authors[0] != "jeremy@geneseesoftware.com" {
		t.Fatalf("authors = %#v", cfg.Sources[0].Settings["authors"])
	}
	if cfg.Sources[3].Settings["token"] != "cmd:gh auth token" {
		t.Fatalf("github token = %#v", cfg.Sources[3].Settings["token"])
	}
	if cfg.Sources[4].Settings["token"] != "" {
		t.Fatalf("linear token = %#v", cfg.Sources[4].Settings["token"])
	}
	if cfg.Sources[4].Settings["ticket_pattern"] != `GEN-\d+` {
		t.Fatalf("ticket_pattern = %#v", cfg.Sources[4].Settings["ticket_pattern"])
	}
	if cfg.Sources[5].Settings["token"] != "" {
		t.Fatalf("cursor-cloud token = %#v", cfg.Sources[5].Settings["token"])
	}
	if cfg.Sources[6].Settings["account_id"] != "123" {
		t.Fatalf("account_id = %#v", cfg.Sources[6].Settings["account_id"])
	}
	if cfg.Sources[6].Settings["token"] != "cmd:op read op://dev/basecamp/token" {
		t.Fatalf("basecamp token = %#v", cfg.Sources[6].Settings["token"])
	}
}

func TestOutputMode(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want string
	}{
		{name: "empty", toml: "[output]\ndir = \"/tmp/weeklog-out\"\n", want: OutputModeBoth},
		{name: "both", toml: "[output]\nmode = \"both\"\n", want: OutputModeBoth},
		{name: "file", toml: "[output]\nmode = \"file\"\n", want: OutputModeFile},
		{name: "terminal", toml: "[output]\nmode = \"terminal\"\n", want: OutputModeTerminal},
		{name: "mixed case", toml: "[output]\nmode = \"File\"\n", want: OutputModeFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, tc.toml))
			if err != nil {
				t.Fatal(err)
			}
			got, err := cfg.Output.ResolvedMode()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("mode = %q, want %q", got, tc.want)
			}
		})
	}

	cfg, err := Load(writeConfig(t, "[output]\nmode = \"desktop\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Output.ResolvedMode(); err == nil || !strings.Contains(err.Error(), "output.mode") {
		t.Fatalf("err = %v", err)
	}
	path := writeConfig(t, "[output]\nmode = 1\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "output.mode") {
		t.Fatalf("err = %v", err)
	}

	checked, err := Check(context.Background(), writeConfig(t, "[output]\ndir = \"/tmp/weeklog-out\"\nmode = \"file\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !checked.OK() || !strings.Contains(checked.Report(), "output.mode: file\n") {
		t.Fatal(checked.Report())
	}
}

func TestLoadDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "should-not-exist")
	body := `
[[source]]
type = "basecamp"
token = "cmd:touch ` + marker + `"
`
	path := writeConfigIn(t, dir, body)
	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Load executed a cmd: secret, stat err = %v", err)
	}
}

func TestLoadDefaultsAndCanonicalValues(t *testing.T) {
	path := writeConfig(t, `
[identity]
timezone = "utc"
week_starts = "Monday"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Identity.Timezone != "UTC" {
		t.Fatalf("timezone = %q", cfg.Identity.Timezone)
	}
	if cfg.Identity.WeekStarts != "monday" {
		t.Fatalf("week_starts = %q", cfg.Identity.WeekStarts)
	}
	loc, err := cfg.Identity.Location()
	if err != nil || loc != time.UTC {
		t.Fatalf("Location() = %v, %v", loc, err)
	}
	day, err := cfg.Identity.WeekStart()
	if err != nil || day != time.Monday {
		t.Fatalf("WeekStart() = %v, %v", day, err)
	}

	path = writeConfig(t, "[identity]\ntimezone = \"local\"\n")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load local: %v", err)
	}
	if cfg.Identity.Timezone != "" || cfg.Identity.WeekStarts != "monday" {
		t.Fatalf("identity = %+v", cfg.Identity)
	}
	loc, err = cfg.Identity.Location()
	if err != nil || loc != time.Local {
		t.Fatalf("local Location() = %v, %v", loc, err)
	}
}

func TestLoadErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("directory", func(t *testing.T) {
		_, err := Load(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "directory") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("syntax", func(t *testing.T) {
		path := writeConfig(t, "[\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "parse config") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unknown keys", func(t *testing.T) {
		path := writeConfig(t, `
foo = "bar"
[identity]
name = "nope"
`)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "unknown config keys") || !strings.Contains(err.Error(), "identity.name") || !strings.Contains(err.Error(), "foo") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		path := writeConfig(t, `
[identity]
emails = "jeremy@example.com"
`)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "identity.emails") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("user path", func(t *testing.T) {
		path := writeConfig(t, `
[output]
dir = "~bob/journal"
`)
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "~user") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("too large", func(t *testing.T) {
		orig := maxConfigBytes
		maxConfigBytes = 8
		t.Cleanup(func() { maxConfigBytes = orig })
		path := writeConfig(t, "output_dir = \"abcdefghij\"\n")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("empty path", func(t *testing.T) {
		_, err := Load("  ")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	xdg := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(xdg, "weeklog", "config.toml")
	if got != want {
		t.Fatalf("DefaultPath = %q, want %q", got, want)
	}
	got, err = ResolveConfigPath("  ")
	if err != nil || got != want {
		t.Fatalf("ResolveConfigPath empty = %q, %v", got, err)
	}
	got, err = ResolveConfigPath("~/custom.toml")
	if err != nil || got != filepath.Join(home, "custom.toml") {
		t.Fatalf("tilde = %q, %v", got, err)
	}
	got, err = ResolveConfigPath("/tmp/weeklog.toml")
	if err != nil || got != "/tmp/weeklog.toml" {
		t.Fatalf("absolute = %q, %v", got, err)
	}

	t.Setenv("XDG_CONFIG_HOME", "placeholder")
	if err := os.Unsetenv("XDG_CONFIG_HOME"); err != nil {
		t.Fatal(err)
	}
	got, err = DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		dir, dirErr := os.UserConfigDir()
		if dirErr != nil {
			t.Fatal(dirErr)
		}
		if got != filepath.Join(dir, "weeklog", "config.toml") {
			t.Fatalf("windows fallback = %q", got)
		}
		return
	}
	unixWant := filepath.Join(home, ".config", "weeklog", "config.toml")
	if got != unixWant {
		t.Fatalf("unix fallback = %q, want %q", got, unixWant)
	}
}

func TestParseSecretRef(t *testing.T) {
	cases := []struct {
		raw     string
		kind    refKind
		display string
		wantErr string
	}{
		{raw: "env:GITHUB_TOKEN", kind: refEnv, display: "env:GITHUB_TOKEN"},
		{raw: "  env:GITHUB_TOKEN  ", kind: refEnv, display: "env:GITHUB_TOKEN"},
		{raw: "env:", kind: refEnv, display: "env:", wantErr: "invalid"},
		{raw: "env:1NO", kind: refEnv, display: "env:", wantErr: "invalid"},
		{raw: "cmd:gh auth token", kind: refCmd, display: "cmd:gh auth token"},
		{raw: "cmd:", kind: refCmd, display: "cmd:", wantErr: "empty"},
		{raw: "file:~/secret", kind: refFile, display: "file:~/secret"},
		{raw: "file:", kind: refFile, display: "file:", wantErr: "empty"},
		{raw: "ghp_plain", kind: refLiteral, display: "literal"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := parseSecretRef(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v", err)
				}
			} else if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got.Kind != tc.kind || got.Display != tc.display {
				t.Fatalf("ref = %+v", got)
			}
		})
	}
}

func TestCheckReport(t *testing.T) {
	const secret = "wksec_ENV_7f3a9c1e"
	t.Setenv("WEEKLOG_TEST_SECRET", secret)
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeConfig(t, `
[identity]
emails = ["a@example.com", "b@example.com"]
github = "jeremyjaymes"
timezone = "America/Chicago"
week_starts = "Monday"

[output]
dir = "~/journal/weeklog"

[redaction]
extra_patterns = ["sk_live_[A-Za-z0-9]+"]
allow = ["example"]

[[source]]
type = "git"
roots = ["~/Projects", "/tmp/repos"]
authors = ["a@example.com"]

[[source]]
type = "github"
token = "env:WEEKLOG_TEST_SECRET"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("issues = %#v", result.Issues)
	}
	gotSecret, ok := result.Secret(1, "token")
	if !ok || gotSecret != secret {
		t.Fatal("resolved secret mismatch")
	}
	want := "" +
		"config: " + path + "\n" +
		"identity.timezone: America/Chicago\n" +
		"identity.week_starts: monday\n" +
		"identity.github: jeremyjaymes\n" +
		"identity.emails: a@example.com, b@example.com\n" +
		"output.dir: " + filepath.Join(home, "journal", "weeklog") + "\n" +
		"output.mode: both\n" +
		"output.template: built-in\n" +
		"redaction.extra_patterns: 1\n" +
		"redaction.allow: 1\n" +
		"source[0] git\n" +
		"source[1] github\n" +
		"  token: resolved (env:WEEKLOG_TEST_SECRET)\n" +
		"ok\n"
	if result.Report() != want {
		t.Fatalf("report mismatch\n got:\n%s\nwant:\n%s", hide(result.Report(), secret), hide(want, secret))
	}
	requireNoSecret(t, secret, "report", result.Report())
}

func TestCheckCases(t *testing.T) {
	const secret = "wksec_ENV_7f3a9c1e"
	const literal = "wksec_LITERAL_bb12dd34"
	const stderrSecret = "wksec_STDERR_c0ffee99"

	cases := []struct {
		name      string
		toml      string
		env       map[string]string
		files     map[string]string
		ok        bool
		want      []string
		absent    []string
		secretIdx int
		secretKey string
		secretVal string
	}{
		{
			name: "cursor-cloud literal token",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "cursor-cloud"
token = "` + literal + `"
`,
			ok:        true,
			want:      []string{"source[0] cursor-cloud", "token: literal", "\nok\n"},
			absent:    []string{literal, "literal secret"},
			secretIdx: 0,
			secretKey: "token",
			secretVal: literal,
		},
		{
			name: "cursor-cloud empty token",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "cursor-cloud"
token = ""
`,
			ok:   false,
			want: []string{"source[0].token: must not be empty"},
		},
		{
			name: "cursor-cloud missing token",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "cursor-cloud"
`,
			ok:   false,
			want: []string{"source[0].token: required field is missing"},
		},
		{
			name: "literal secret",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "` + literal + `"
`,
			ok:        true,
			want:      []string{"token: literal", "\nok\n"},
			absent:    []string{literal, "literal secret"},
			secretIdx: 0,
			secretKey: "token",
			secretVal: literal,
		},
		{
			name: "env unset",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "linear"
token = "env:WEEKLOG_MISSING_SECRET"
ticket_pattern = "GEN-\\d+"
`,
			ok:   false,
			want: []string{"unresolved (env:WEEKLOG_MISSING_SECRET)", "environment variable WEEKLOG_MISSING_SECRET is unset", "1 error"},
		},
		{
			name: "env empty",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "env:WEEKLOG_TEST_SECRET"
`,
			env:  map[string]string{"WEEKLOG_TEST_SECRET": "  "},
			ok:   false,
			want: []string{"environment variable WEEKLOG_TEST_SECRET is empty"},
		},
		{
			name: "file and cmd",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "linear"
token = "file:secret.txt"
ticket_pattern = "GEN-\\d+"
[[source]]
type = "basecamp"
account_id = "123"
token = "cmd:printenv WEEKLOG_TEST_SECRET"
`,
			env:       map[string]string{"WEEKLOG_TEST_SECRET": secret},
			files:     map[string]string{"secret.txt": secret + "\n"},
			ok:        true,
			want:      []string{"resolved (file:secret.txt)", "resolved (cmd:printenv WEEKLOG_TEST_SECRET)", "ok"},
			absent:    []string{secret},
			secretIdx: 0,
			secretKey: "token",
			secretVal: secret,
		},
		{
			name: "cmd stderr discarded",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "cmd:sh -c 'printf %s \"$WEEKLOG_TEST_STDERR\" >&2; exit 9'"
`,
			env:    map[string]string{"WEEKLOG_TEST_STDERR": stderrSecret},
			ok:     false,
			want:   []string{"command exited 9"},
			absent: []string{stderrSecret},
		},
		{
			name: "cmd empty",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "cmd:true"
`,
			ok:   false,
			want: []string{"command produced empty output"},
		},
		{
			name: "bad refs",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "basecamp"
token = "env:"
api_key = "cmd:"
password = "file:"
`,
			ok:   false,
			want: []string{"invalid environment variable name", "command is empty", "file path is empty", "3 errors"},
		},
		{
			name: "empty token",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "  "
`,
			ok:   false,
			want: []string{"source[0].token: must not be empty"},
		},
		{
			name: "git rules",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "git"
[[source]]
type = "git"
roots = []
[[source]]
type = "git"
roots = [""]
[[source]]
type = "git"
roots = "Projects"
[[source]]
type = "Git"
roots = ["/tmp"]
[[source]]
type = "git"
roots = ["/tmp"]
depth = 3
`,
			ok: false,
			want: []string{
				"source[0].roots: required field is missing",
				"source[1].roots: must contain at least one value",
				"source[2].roots[0]: must not be empty",
				"source[3].roots: expected an array of strings",
				`source type "Git" must be "git"`,
				"source[5].depth: unknown field for git",
			},
		},
		{
			name: "linear and redaction",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[redaction]
extra_patterns = ["("]
allow = [""]
[[source]]
type = "linear"
ticket_pattern = "("
`,
			ok: false,
			want: []string{
				"redaction.extra_patterns[0]: invalid regular expression",
				"redaction.allow[0]: must not be empty",
				"source[0].token: required field is missing",
				"source[0].ticket_pattern: invalid regular expression",
			},
		},
		{
			name: "identity and warnings",
			toml: `
[identity]
emails = ["", "bad email"]
github = "jer emy"
timezone = "Not/AZone"
week_starts = "funday"
[[source]]
type = "not a type"
`,
			ok: false,
			want: []string{
				"invalid timezone",
				"invalid week_starts",
				"identity.emails[0]: must not be empty",
				"identity.emails[1]: must not contain whitespace",
				"identity.github: must not contain whitespace",
				"invalid source type",
				"output.dir: is unset",
			},
		},
		{
			name: "bad output mode",
			toml: `
[output]
dir = "/tmp/weeklog-out"
mode = "desktop"
`,
			ok:   false,
			want: []string{`invalid output.mode "desktop"`, "error: output.mode:"},
		},
		{
			name: "no sources",
			toml: `
[output]
dir = "/tmp/weeklog-out"
`,
			ok:   true,
			want: []string{"warning: sources: no sources configured", "1 warning"},
		},
		{
			name: "claude and codex",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "claude-code"
[[source]]
type = "codex"
`,
			ok:   true,
			want: []string{"source[0] claude-code", "source[1] codex", "ok"},
		},
		{
			name: "two git sources",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "git"
roots = ["/tmp/a"]
[[source]]
type = "git"
roots = ["/tmp/b"]
authors = ["a@example.com"]
`,
			ok:   true,
			want: []string{"source[0] git", "source[1] git", "ok"},
		},
		{
			name: "external secret type",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "basecamp"
token = 1
`,
			ok:   false,
			want: []string{"source[0].token: expected a string"},
		},
		{
			name: "missing file",
			toml: `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "file:missing-secret.txt"
`,
			ok:   false,
			want: []string{"file does not exist"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			dir := t.TempDir()
			for name, body := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			path := writeConfigIn(t, dir, tc.toml)
			result, err := Check(context.Background(), path)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if result.OK() != tc.ok {
				t.Fatalf("OK = %v, issues = %#v\n%s", result.OK(), result.Issues, hide(result.Report(), secret, literal, stderrSecret))
			}
			report := result.Report()
			for _, want := range tc.want {
				if !strings.Contains(report, want) {
					t.Fatalf("report missing %q\n%s", want, hide(report, secret, literal, stderrSecret))
				}
			}
			for _, banned := range tc.absent {
				requireNoSecret(t, banned, "report", report)
			}
			if tc.secretKey != "" {
				got, ok := result.Secret(tc.secretIdx, tc.secretKey)
				if !ok || got != tc.secretVal {
					t.Fatal("resolved secret mismatch")
				}
			}
		})
	}
}

func TestSecretFileAndCommandLimits(t *testing.T) {
	const fileSecret = "wksec_FILE_LIMIT_aa"
	const cmdSecret = "wksec_CMD_LIMIT_bb"
	orig := maxSecretBytes
	maxSecretBytes = 4
	t.Cleanup(func() { maxSecretBytes = orig })
	t.Setenv("WEEKLOG_BIG_SECRET", cmdSecret)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(fileSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "file:big.txt"
[[source]]
type = "basecamp"
token = "cmd:printenv WEEKLOG_BIG_SECRET"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report()
	if strings.Contains(report, fileSecret) || strings.Contains(report, cmdSecret) {
		t.Fatal("report contains secret payload")
	}
	if !strings.Contains(report, "exceeds 4 bytes") {
		t.Fatalf("report = %s", report)
	}
	if _, ok := result.Secret(0, "token"); ok {
		t.Fatal("oversized file secret was returned")
	}
}

func TestRunCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep timeout is covered on unix")
	}
	orig := commandTimeout
	commandTimeout = 200 * time.Millisecond
	t.Cleanup(func() { commandTimeout = orig })

	_, err := runCommand(context.Background(), "sleep 5")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "sleep") {
		t.Fatalf("timeout error leaked the command: %v", err)
	}
}

func TestRelativeAndHomeSecretFiles(t *testing.T) {
	const secret = "wksec_FILE_4455aa"
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "home-secret"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "file:~/home-secret"
[[source]]
type = "linear"
token = "file:nested"
ticket_pattern = "AB-\\d+"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.Secret(0, "token")
	if !ok || got != secret {
		t.Fatal("home file secret mismatch")
	}
	requireNoSecret(t, secret, "report", result.Report())
	if !strings.Contains(result.Report(), "file path is a directory") {
		t.Fatalf("report = %s", hide(result.Report(), secret))
	}
}

func TestEmptyFileSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "file:empty.txt"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || !strings.Contains(result.Report(), "file is empty") {
		t.Fatalf("report = %s", result.Report())
	}
}

func TestCursorCloudSecretRefs(t *testing.T) {
	const secret = "wksec_CURSOR_ref_91"
	t.Setenv("WEEKLOG_CURSOR_TOKEN", secret)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cursor.txt"), []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "cursor-cloud"
token = "env:WEEKLOG_CURSOR_TOKEN"
[[source]]
type = "cursor-cloud"
token = "file:cursor.txt"
[[source]]
type = "cursor-cloud"
token = "cmd:printenv WEEKLOG_CURSOR_TOKEN"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("report = %s", result.Report())
	}
	requireNoSecret(t, secret, "report", result.Report())
	for _, want := range []string{
		"resolved (env:WEEKLOG_CURSOR_TOKEN)",
		"resolved (file:cursor.txt)",
		"resolved (cmd:printenv WEEKLOG_CURSOR_TOKEN)",
	} {
		if !strings.Contains(result.Report(), want) {
			t.Fatalf("report missing %q\n%s", want, result.Report())
		}
	}
	for i := 0; i < 3; i++ {
		got, ok := result.Secret(i, "token")
		if !ok || got != secret {
			t.Fatalf("source %d token = %q ok=%v", i, got, ok)
		}
	}
}

func TestCheckLoopbackEndpoint(t *testing.T) {
	const secret = "wksec_LOOP_ENDPOINT_1a"
	t.Setenv("WEEKLOG_TEST_SECRET", secret)
	path := writeConfig(t, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "env:WEEKLOG_TEST_SECRET"
endpoint = " http://127.0.0.1:9/graphql "
[[source]]
type = "linear"
token = "env:WEEKLOG_TEST_SECRET"
endpoint = "http://localhost:9"
[[source]]
type = "cursor-cloud"
token = "env:WEEKLOG_TEST_SECRET"
endpoint = "http://[::1]:9/v1"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("report = %s", result.Report())
	}
	if strings.Contains(result.Report(), secret) || strings.Contains(result.Report(), "127.0.0.1") {
		t.Fatalf("report = %s", hide(result.Report(), secret))
	}
	github, ok := result.PluginConfig(0)
	if !ok || github["endpoint"] != " http://127.0.0.1:9/graphql " || github["token"] != secret {
		t.Fatal("github plugin config did not keep the loopback endpoint and resolved token")
	}
	linear, ok := result.PluginConfig(1)
	if !ok || linear["endpoint"] != "http://localhost:9" || linear["token"] != secret {
		t.Fatal("linear plugin config did not keep the loopback endpoint and resolved token")
	}
	cloud, ok := result.PluginConfig(2)
	if !ok || cloud["endpoint"] != "http://[::1]:9/v1" || cloud["token"] != secret {
		t.Fatal("cursor-cloud plugin config did not keep the loopback endpoint and resolved token")
	}

	rejected := writeConfig(t, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
endpoint = "https://127.0.0.1/graphql"
[[source]]
type = "linear"
token = "env:WEEKLOG_TEST_SECRET"
endpoint = "http://example.com/graphql"
[[source]]
type = "git"
roots = ["/tmp"]
endpoint = "http://127.0.0.1:9"
[[source]]
type = "cursor-cloud"
token = "env:WEEKLOG_TEST_SECRET"
endpoint = "https://api.cursor.com"
`)
	result, err = Check(context.Background(), rejected)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report()
	if result.OK() || strings.Contains(report, secret) {
		t.Fatalf("report = %s", hide(report, secret))
	}
	for _, want := range []string{
		"source[0].endpoint: must be an http URL on 127.0.0.1, localhost, or ::1",
		"source[1].endpoint: must be an http URL on 127.0.0.1, localhost, or ::1",
		"source[2].endpoint: unknown field for git",
		"source[3].endpoint: must be an http URL on 127.0.0.1, localhost, or ::1",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q\n%s", want, report)
		}
	}
}

func TestCheckWarnsWhenConfigIsGroupOrWorldReadable(t *testing.T) {
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

	cases := []struct {
		mode os.FileMode
		warn bool
	}{
		{mode: 0o644, warn: true},
		{mode: 0o640, warn: true},
		{mode: 0o604, warn: true},
		{mode: 0o600, warn: false},
		{mode: 0o700, warn: false},
		{mode: 0o620, warn: false},
	}
	for _, tc := range cases {
		t.Run(tc.mode.String(), func(t *testing.T) {
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			result, err := Check(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if !result.OK() {
				t.Fatalf("issues = %#v", result.Issues)
			}
			report := result.Report()
			requireNoSecret(t, literal, "report", report)
			for _, issue := range result.Issues {
				requireNoSecret(t, literal, "issue", issue.Field+" "+issue.Message)
			}
			warned := strings.Contains(report, "warning: config: file is group- or world-readable")
			if warned != tc.warn {
				t.Fatalf("mode %o warned=%v\n%s", tc.mode, warned, report)
			}
			if !strings.Contains(report, "token: literal") || strings.Contains(report, "literal secret") {
				t.Fatalf("report = %s", report)
			}
			got, ok := result.Secret(0, "token")
			if !ok || got != literal {
				t.Fatal("resolved secret mismatch")
			}
		})
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	return writeConfigIn(t, t.TempDir(), body)
}

func writeConfigIn(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func requireNoSecret(t *testing.T, secret, label, text string) {
	t.Helper()
	if secret != "" && strings.Contains(text, secret) {
		t.Fatalf("%s contains secret value\n%s", label, hide(text, secret))
	}
}

func hide(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[canary]")
	}
	return text
}
