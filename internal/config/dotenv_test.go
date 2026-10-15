package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/redact"
)

func TestParseDotEnv(t *testing.T) {
	const body = "" +
		"\uFEFF# comment\n" +
		"\n" +
		"export LINEAR_API_KEY=lin_from_export\n" +
		"GITHUB_TOKEN = \"tok with space\"\n" +
		"QUOTED='keep # hash'\n" +
		"BARE=value # trailing\n" +
		"HASH=#not-a-comment\n" +
		"EMPTY=\n" +
		"SPACED= # comment\n" +
		"export\tESCAPED=\"a\\nb\\\"c\"\n" +
		"LINEAR_API_KEY=lin_last_wins\r\n"
	got, err := parseDotEnv(body)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"LINEAR_API_KEY": "lin_last_wins",
		"GITHUB_TOKEN":   "tok with space",
		"QUOTED":         "keep # hash",
		"BARE":           "value",
		"HASH":           "#not-a-comment",
		"EMPTY":          "",
		"SPACED":         "",
		"ESCAPED":        "a\nb\"c",
	}
	if len(got) != len(want) {
		t.Fatalf("keys = %#v", got)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %q", key, got[key])
		}
	}
}

func TestParseDotEnvRejectsSecretsInErrors(t *testing.T) {
	const secret = "wksec_DOTENV_PARSE_9911"
	_, err := parseDotEnv("1BAD=" + secret + "\n")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Fatal(err)
	}
	_, err = parseDotEnv("OK=fine\nUNCLOSED=\"" + secret)
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
	if _, err := parseDotEnv("OK=a\x00" + secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("nul err = %v", err)
	}
}

func TestDotEnvResolution(t *testing.T) {
	const (
		fromFile = "wksec_DOTENV_FILE_aa11"
		fromEnv  = "wksec_DOTENV_ENV_bb22"
		extra    = "wksec_DOTENV_EXTRA_cc33"
		fileOnly = "WEEKLOG_DOTENV_FILE_ONLY"
		both     = "WEEKLOG_DOTENV_BOTH"
		extraKey = "WEEKLOG_DOTENV_EXTRA"
	)
	t.Setenv(fileOnly, "sentinel")
	if err := os.Unsetenv(fileOnly); err != nil {
		t.Fatal(err)
	}
	t.Setenv(both, fromEnv)

	dir := t.TempDir()
	envBody := fileOnly + "=" + fromFile + "\n" +
		both + "=wksec_DOTENV_LOSER_dd44\n" +
		extraKey + "=" + extra + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envBody), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "linear"
token = "env:`+fileOnly+`"
[[source]]
type = "github"
token = "env:`+both+`"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatal(result.Report())
	}
	gotFile, ok := result.Secret(0, "token")
	if !ok || gotFile != fromFile {
		t.Fatalf("file secret = %q ok=%v", gotFile, ok)
	}
	gotEnv, ok := result.Secret(1, "token")
	if !ok || gotEnv != fromEnv {
		t.Fatalf("env secret = %q ok=%v", gotEnv, ok)
	}
	report := result.Report()
	for _, secret := range []string{fromFile, fromEnv, extra, "wksec_DOTENV_LOSER_dd44"} {
		requireNoSecret(t, secret, "report", report)
	}
	if !strings.Contains(report, "resolved (env:"+fileOnly+" from .env)") {
		t.Fatalf("report = %s", report)
	}
	if !strings.Contains(report, "resolved (env:"+both+")") || strings.Contains(report, "resolved (env:"+both+" from .env)") {
		t.Fatalf("report = %s", report)
	}
	values := result.SecretValues()
	for _, secret := range []string{fromFile, fromEnv, extra, "wksec_DOTENV_LOSER_dd44"} {
		if !containsString(values, secret) {
			t.Fatalf("SecretValues missing %s: %#v", secret, values)
		}
	}
	redactor, err := redact.New(redact.Options{Secrets: values})
	if err != nil {
		t.Fatal(err)
	}
	text := redactor.Text("see " + fromFile + " " + fromEnv + " " + extra + " wksec_DOTENV_LOSER_dd44")
	for _, secret := range []string{fromFile, fromEnv, extra, "wksec_DOTENV_LOSER_dd44"} {
		if strings.Contains(text, secret) {
			t.Fatalf("redaction kept %s in %s", secret, text)
		}
	}

	other := writeConfigIn(t, t.TempDir(), `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "env:`+fileOnly+`"
`)
	miss, err := Check(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	if miss.OK() || !strings.Contains(miss.Report(), "environment variable "+fileOnly+" is unset") {
		t.Fatal(miss.Report())
	}
	if strings.Contains(miss.Report(), fromFile) {
		t.Fatal(miss.Report())
	}
}

func TestDotEnvEmptyFileLosesToEmptyEnvironment(t *testing.T) {
	const name = "WEEKLOG_DOTENV_EMPTY"
	t.Setenv(name, "   ")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(name+"=wksec_DOTENV_SHOULD_NOT_WIN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "env:`+name+`"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report()
	if result.OK() || !strings.Contains(report, "environment variable "+name+" is empty") || strings.Contains(report, "in .env") {
		t.Fatalf("report = %s", report)
	}
	if strings.Contains(report, "wksec_DOTENV_SHOULD_NOT_WIN") {
		t.Fatal(report)
	}
}

func TestDotEnvEmptyValue(t *testing.T) {
	const name = "WEEKLOG_DOTENV_BLANK"
	t.Setenv(name, "sentinel")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(name+"=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "env:`+name+`"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || !strings.Contains(result.Report(), "environment variable "+name+" is empty in .env") {
		t.Fatal(result.Report())
	}
}

func TestDotEnvPermissionWarning(t *testing.T) {
	const secret = "wksec_DOTENV_MODE_ee55"
	const name = "WEEKLOG_DOTENV_MODE"
	t.Setenv(name, "sentinel")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte(name+"="+secret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(envPath, 0o644); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "env:`+name+`"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report()
	requireNoSecret(t, secret, "report", report)
	if !result.OK() {
		t.Fatal(report)
	}
	if !strings.Contains(report, "from .env") {
		t.Fatal(report)
	}
	if runtime.GOOS == "windows" {
		if strings.Contains(report, "group- or world-readable") {
			t.Fatal(report)
		}
	} else if !strings.Contains(report, "warning: .env: file is group- or world-readable") {
		t.Fatal(report)
	}

	if err := os.Chmod(envPath, 0o600); err != nil {
		t.Fatal(err)
	}
	tight, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tight.Report(), "group- or world-readable") {
		t.Fatal(tight.Report())
	}
}

func TestDotEnvUnreadableShapes(t *testing.T) {
	const secret = "wksec_DOTENV_BAD_ff66"
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
[[source]]
type = "github"
token = "env:WEEKLOG_DOTENV_ABSENT"
`)
	result, err := Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || !strings.Contains(result.Report(), ".env: path is a directory") {
		t.Fatal(result.Report())
	}

	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("NOT A "+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path = writeConfigIn(t, dir, `
[output]
dir = "/tmp/weeklog-out"
`)
	result, err = Check(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report()
	if result.OK() || !strings.Contains(report, "invalid .env entry on line 1") || strings.Contains(report, secret) {
		t.Fatalf("report = %s", report)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
