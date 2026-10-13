package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestRegistryDiscoversPluginsAndHidesShadows(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins")
	pathDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, pluginDir, "todoist", `printf '%s\n' '{"protocol":1,"name":"todoist","version":"9.9.9","config":{}}'`+"\n")
	writeScript(t, pathDir, "todoist", `printf '%s\n' '{"protocol":1,"name":"todoist","version":"1.0.0","config":{}}'`+"\n")
	writeScript(t, pathDir, "git", "touch \"$WEEKLOG_SHADOW_TOUCH\"\nexit 1\n")
	if err := os.WriteFile(filepath.Join(pathDir, PluginPrefix+"plain"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shadow := filepath.Join(root, "shadow-touch")
	t.Setenv("WEEKLOG_SHADOW_TOUCH", shadow)

	reg := NewRegistry()
	if err := reg.Register("git", fakeSource{name: "git"}); err != nil {
		t.Fatal(err)
	}
	reg.SetPluginDir(pluginDir)
	reg.SetPath(pathDir)
	reg.SetTimeout(5 * time.Second)

	list, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sources) != 2 {
		t.Fatalf("sources = %#v", list.Sources)
	}
	if list.Sources[0].Name != "git" || list.Sources[0].Kind != KindBuiltin || list.Sources[0].Version != BuiltinVersion {
		t.Fatalf("builtin = %#v", list.Sources[0])
	}
	if list.Sources[1].Name != "todoist" || list.Sources[1].Kind != KindPlugin || list.Sources[1].Version != "9.9.9" {
		t.Fatalf("plugin = %#v", list.Sources[1])
	}
	if !strings.HasPrefix(list.Sources[1].Path, pluginDir) {
		t.Fatalf("plugin path = %s", list.Sources[1].Path)
	}
	var sawShadow, sawPlain bool
	for _, item := range list.Ignored {
		if item.Name == "git" && strings.Contains(item.Reason, "shadows built-in") {
			sawShadow = true
		}
		if item.Name == "plain" && item.Reason == "not executable" {
			sawPlain = true
		}
	}
	if !sawShadow || !sawPlain {
		t.Fatalf("ignored = %#v", list.Ignored)
	}
	if _, err := os.Stat(shadow); !os.IsNotExist(err) {
		t.Fatal("shadowing plugin was executed")
	}

	opened, err := reg.Open("todoist")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := opened.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "9.9.9" {
		t.Fatalf("opened version = %s", manifest.Version)
	}
	if _, err := reg.Open("git"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Open("missing"); err == nil {
		t.Fatal("expected missing source")
	}
}

func TestDoctorReportsProtocolAndConfig(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, pluginDir, "todoist", `printf '%s\n' '{"protocol":1,"name":"todoist","version":"0.1.0","config":{"token":{"type":"secret","required":true}}}'`+"\n")
	writeScript(t, pluginDir, "broken", "echo nope\nexit 1\n")

	reg := NewRegistry()
	if err := reg.Register("git", fakeSource{name: "git"}); err != nil {
		t.Fatal(err)
	}
	reg.SetPluginDir(pluginDir)
	reg.SetPath("")
	reg.SetTimeout(5 * time.Second)

	report, err := reg.Doctor(context.Background(), DoctorOptions{
		ConfigPath:    filepath.Join(root, "config.toml"),
		ConfigMissing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatal("broken plugin should fail doctor")
	}
	text := report.Text()
	for _, want := range []string{
		"(not found)",
		"source git: ok (builtin, protocol 1, version " + BuiltinVersion + ")",
		"source todoist: ok (plugin, protocol 1, version 0.1.0",
		"source broken: error:",
		"1 error",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q\n%s", want, text)
		}
	}

	report, err = reg.Doctor(context.Background(), DoctorOptions{
		ConfigPath: filepath.Join(root, "config.toml"),
		Configured: []Configured{{Index: 0, Name: "git"}, {Index: 1, Name: "basecamp"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() || !strings.Contains(report.Text(), "source[1] basecamp: error: plugin not found") {
		t.Fatalf("report:\n%s", report.Text())
	}
}

func TestDoctorPluginDirPermissions(t *testing.T) {
	if privileged() {
		t.Skip("root ignores directory mode bits")
	}
	dir := filepath.Join(t.TempDir(), "plugins")
	if err := os.Mkdir(dir, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry()
	reg.SetPluginDir(dir)
	reg.SetPath("")
	report, err := reg.Doctor(context.Background(), DoctorOptions{
		ConfigPath: filepath.Join(t.TempDir(), "no.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() || !strings.Contains(report.Text(), "not searchable") {
		t.Fatalf("report:\n%s", report.Text())
	}
}

func TestFormatSources(t *testing.T) {
	text := FormatSources([]Info{
		{Name: "git", Kind: KindBuiltin, Protocol: 1, Version: "0.0.0"},
		{Name: "bad", Kind: KindPlugin, Path: "/tmp/weeklog-source-bad", Err: errors.New("describe failed")},
	})
	if !strings.Contains(text, "NAME\tKIND\tSTATUS\tVERSION\tLOCATION\n") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "git\tbuiltin\tok\t0.0.0\tbuilt-in\n") {
		t.Fatalf("text = %q", text)
	}
	if !strings.Contains(text, "bad\tplugin\terror\t-\t/tmp/weeklog-source-bad\n  describe failed\n") {
		t.Fatalf("text = %q", text)
	}
}

type fakeSource struct {
	name string
}

func (f fakeSource) Describe(ctx context.Context) (Manifest, error) {
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	return Manifest{Protocol: ProtocolVersion, Name: f.name, Version: BuiltinVersion, Config: map[string]Field{}}, nil
}

func (f fakeSource) Collect(ctx context.Context, _ CollectRequest, _ func(event.Event) error) (Result, error) {
	return Unimplemented{Name: f.name}.Collect(ctx, CollectRequest{}, nil)
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, PluginPrefix+name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
