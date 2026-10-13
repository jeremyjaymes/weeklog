package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	t.Run("default is dev", func(t *testing.T) {
		if version != "dev" {
			t.Fatalf("default version = %q, want %q", version, "dev")
		}
		got := runVersion(t)
		if got != "weeklog version dev" {
			t.Fatalf("version output = %q, want %q", got, "weeklog version dev")
		}
	})

	t.Run("prints linked version", func(t *testing.T) {
		original := version
		version = "v1.2.3"
		t.Cleanup(func() { version = original })

		got := runVersion(t)
		if got != "weeklog version v1.2.3" {
			t.Fatalf("version output = %q, want %q", got, "weeklog version v1.2.3")
		}
	})
}

func runVersion(t *testing.T) string {
	t.Helper()

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("weeklog version: %v\nstderr: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}
