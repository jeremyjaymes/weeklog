//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourcesAndDoctorReportWritablePlugin(t *testing.T) {
	home := isolatePlugins(t)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "weeklog-source-loose")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o777); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runWeeklog(t, "sources")
	if err != nil {
		t.Fatalf("sources: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, path) || !strings.Contains(stdout, "group/world-writable") {
		t.Fatalf("stdout = %s", stdout)
	}
	if strings.Contains(stdout, "loose\tplugin") {
		t.Fatalf("writable plugin was listed\n%s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, path) || !strings.Contains(stdout, "group/world-writable") {
		t.Fatalf("stdout = %s", stdout)
	}
}
