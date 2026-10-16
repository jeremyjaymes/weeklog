package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecapImagePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got, err := recapImagePath("~/weeks/recap.PNG")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "weeks", "recap.PNG")
	if got != want {
		t.Fatalf("path = %s, want %s", got, want)
	}

	for _, path := range []string{"", "-", "recap.webp", "recap", "recap.jpg"} {
		_, err := recapImagePath(path)
		if err == nil || !strings.Contains(err.Error(), ".png or .svg") {
			t.Fatalf("path %q err = %v", path, err)
		}
	}
}

func TestWeekImageNeedsFreeze(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	out := filepath.Join(home, "week.md")
	image := filepath.Join(home, "recap.png")

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--out", out, "--image", image)
	if err == nil || !strings.Contains(err.Error(), "freeze is not installed") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	for _, want := range []string{"PATH", "go install github.com/charmbracelet/freeze@latest", "brew install charmbracelet/tap/freeze", "https://github.com/charmbracelet/freeze"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q\n%s", want, err.Error())
		}
	}
	if stdout != "" {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("wrote the report without freeze: %v", statErr)
	}
	if _, statErr := os.Stat(image); !os.IsNotExist(statErr) {
		t.Fatalf("wrote an image without freeze: %v", statErr)
	}

	_, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--image", filepath.Join(home, "recap.jpg"))
	if err == nil || !strings.Contains(err.Error(), "must end in .png or .svg") || strings.Contains(err.Error(), "freeze is not installed") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
}
