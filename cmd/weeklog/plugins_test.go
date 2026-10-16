package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/sources/claudecode"
	"github.com/jeremyjaymes/weeklog/internal/sources/codex"
	"github.com/jeremyjaymes/weeklog/internal/sources/cursorcloud"
	"github.com/jeremyjaymes/weeklog/internal/sources/git"
	"github.com/jeremyjaymes/weeklog/internal/sources/github"
	"github.com/jeremyjaymes/weeklog/internal/sources/linear"
)

func TestSourcesListsBuiltins(t *testing.T) {
	home := isolatePlugins(t)
	stdout, stderr, err := runWeeklog(t, "sources")
	if err != nil {
		t.Fatalf("sources: %v\n%s\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	for _, name := range []string{"git", "claude-code", "codex", "github", "linear", "cursor-cloud"} {
		if !strings.Contains(stdout, name+"\tbuiltin\tok\t"+builtinDescribedVersion(name)+"\tbuilt-in\n") {
			t.Fatalf("stdout missing %s\n%s", name, stdout)
		}
	}
	if strings.Contains(stdout, "plugin") {
		t.Fatalf("isolated PATH listed a plugin\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "weeklog", "plugins")); !os.IsNotExist(err) {
		t.Fatal("sources created the plugin directory")
	}
}

func TestDoctorWarnsWhenConfigAndPluginDirAreMissing(t *testing.T) {
	isolatePlugins(t)
	stdout, stderr, err := runWeeklog(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	for _, want := range []string{
		"(not found)",
		"source git: ok (builtin, protocol 1, version " + git.Version + ")",
		"source linear: ok (builtin, protocol 1, version " + linear.Version + ")",
		"2 warnings",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q\n%s", want, stdout)
		}
	}
}

func TestDoctorPluginProtocolAndSecretSilence(t *testing.T) {
	const secret = "wksec_DOCTOR_must_not_print"
	home := isolatePlugins(t)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" + `printf '%s\n' '{"protocol":1,"name":"todoist","version":"0.1.0","config":{"token":{"type":"secret","required":true}}}'` + "\n"
	if err := os.WriteFile(filepath.Join(bin, "weeklog-source-todoist"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "secret-touched")
	body := "[[source]]\ntype = \"basecamp\"\ntoken = \"cmd:touch " + marker + "\"\napi_key = \"" + secret + "\"\n"
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runWeeklog(t, "doctor", "--config", path)
	if err == nil {
		t.Fatal("expected missing basecamp to fail")
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatal("doctor printed a config secret")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("doctor executed a cmd: secret")
	}
	if !strings.Contains(stdout, "source todoist: ok (plugin, protocol 1, version 0.1.0") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stdout, "source[0] basecamp: error: plugin not found") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(stderr, "doctor failed") || strings.Contains(stderr, "Usage:") {
		t.Fatalf("stderr = %q", stderr)
	}

	stdout, stderr, err = runWeeklog(t, "sources")
	if err != nil {
		t.Fatalf("sources: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "todoist\tplugin\tok\t0.1.0\t") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestDoctorTimeoutAndParseError(t *testing.T) {
	home := isolatePlugins(t)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nexec /bin/sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "weeklog-source-slow"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runWeeklog(t, "sources", "--timeout", "1s")
	if err != nil {
		t.Fatalf("sources: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "slow\tplugin\terror") || !strings.Contains(stdout, "timed out") {
		t.Fatalf("stdout = %s", stdout)
	}

	const secret = "wksec_PARSE_must_not_print"
	path := filepath.Join(home, "bad.toml")
	if err := os.WriteFile(path, []byte("token = \""+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runWeeklog(t, "doctor", "--config", path, "--timeout", "1s")
	if err == nil {
		t.Fatal("expected parse failure")
	}
	combined := stdout + stderr
	if strings.Contains(combined, secret) {
		t.Fatal("doctor printed a parse secret")
	}
	if !strings.Contains(stdout, "cannot parse config") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestBuiltinsDescribeThroughProtocol(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, builtin := range builtinSources() {
		t.Run(builtin.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "describe")
			cmd.Env = append(os.Environ(), "WEEKLOG_SERVE_BUILTIN="+builtin.name)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("describe: %v", err)
			}
			manifest, err := source.DecodeManifest(out)
			if err != nil {
				t.Fatalf("decode: %v\n%s", err, out)
			}
			if manifest.Name != builtin.name || manifest.Protocol != source.ProtocolVersion || manifest.Version != builtinDescribedVersion(builtin.name) {
				t.Fatalf("manifest = %#v", manifest)
			}
			_, err = builtin.src.Collect(context.Background(), source.CollectRequest{}, nil)
			if builtin.name == "git" || builtin.name == "claude-code" || builtin.name == "codex" || builtin.name == "github" || builtin.name == "linear" || builtin.name == "cursor-cloud" {
				if !errors.Is(err, source.ErrConfig) {
					t.Fatalf("collect err = %v, want config error", err)
				}
				return
			}
			if !errors.Is(err, source.ErrNotImplemented) {
				t.Fatalf("collect err = %v", err)
			}
		})
	}
}

func TestSourcesAndDoctorHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "sources", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "weeklog-source-") || !strings.Contains(stdout, "timeout") {
		t.Fatalf("help = %s\n%s", stdout, stderr)
	}
	stdout, stderr, err = runWeeklog(t, "doctor", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "does not resolve secrets") {
		t.Fatalf("help = %s\n%s", stdout, stderr)
	}
}

func builtinDescribedVersion(name string) string {
	switch name {
	case "git":
		return git.Version
	case "claude-code":
		return claudecode.Version
	case "codex":
		return codex.Version
	case "github":
		return github.Version
	case "linear":
		return linear.Version
	case "cursor-cloud":
		return cursorcloud.Version
	default:
		return source.BuiltinVersion
	}
}

func isolatePlugins(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("PATH", filepath.Join(home, "bin"))
	return home
}
