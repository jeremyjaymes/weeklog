//go:build unix

package source

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDiscoverRejectsGroupWorldWritablePlugins(t *testing.T) {
	root := t.TempDir()
	safeDir := filepath.Join(root, "safe")
	looseDir := filepath.Join(root, "loose")
	if err := os.MkdirAll(safeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(looseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScript(t, safeDir, "good", "printf '%s\\n' '{\"protocol\":1,\"name\":\"good\",\"version\":\"0.1.0\",\"config\":{}}'\n")
	touched := filepath.Join(root, "ran")
	touch := fmt.Sprintf("touch %s\n", strconv.Quote(touched))
	looseFile := writeScript(t, safeDir, "loose", touch)
	if err := os.Chmod(looseFile, 0o777); err != nil {
		t.Fatal(err)
	}
	writeScript(t, looseDir, "indir", touch)
	if err := os.Chmod(looseDir, 0o777); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	reg.SetPluginDir(safeDir)
	reg.SetPath(looseDir)
	reg.SetTimeout(5 * time.Second)
	list, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var sawGood bool
	for _, info := range list.Sources {
		if info.Name == "good" && info.Kind == KindPlugin && info.Err == nil && info.Version == "0.1.0" {
			sawGood = true
		}
		if info.Name == "loose" || info.Name == "indir" {
			t.Fatalf("writable plugin was discovered: %#v", info)
		}
	}
	if !sawGood {
		t.Fatalf("0755 plugin was not discovered: %#v", list.Sources)
	}

	sawLoose, sawDir := false, false
	for _, item := range list.Ignored {
		if item.Name == "loose" && item.Reason == "group/world-writable" {
			sawLoose = true
		}
		if item.Name == "indir" && item.Reason == "group/world-writable" {
			sawDir = true
		}
	}
	if !sawLoose || !sawDir {
		t.Fatalf("ignored = %#v", list.Ignored)
	}
	if _, err := os.Stat(touched); !os.IsNotExist(err) {
		t.Fatal("writable plugin was executed")
	}

	report, err := reg.Doctor(context.Background(), DoctorOptions{
		ConfigPath:    filepath.Join(root, "config.toml"),
		ConfigMissing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := report.Text()
	if !strings.Contains(text, looseFile) || !strings.Contains(text, "group/world-writable") {
		t.Fatalf("report:\n%s", text)
	}
}
