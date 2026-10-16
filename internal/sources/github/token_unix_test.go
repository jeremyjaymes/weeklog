//go:build unix

package github

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectFallsBackToGHAuthToken(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = auth ] && [ \"$2\" = token ]; then\n  printf '%s\\n' 'fixture-gh-token'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	srv, gotReqs := newFixtureServer(t)
	src := New()
	src.endpoint = srv.URL
	events, err := collect(t, src, request("", "alice"))
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureEvents(t, events)
	if gotReqs()[0].Auth != "Bearer fixture-gh-token" {
		t.Fatalf("auth = %q", gotReqs()[0].Auth)
	}

	fail := "#!/bin/sh\nprintf '%s\\n' 'should-not-be-used'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fail), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = collect(t, src, request("", "alice"))
	if err == nil || gotReqs()[len(gotReqs())-1].Auth == "Bearer should-not-be-used" {
		t.Fatalf("failed gh was used: %v", err)
	}
}

func TestGHAuthTokenDiscardsStderr(t *testing.T) {
	secret := "ghp_" + strings.Repeat("Z", 36)
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '" + secret + "' >&2\nprintf '%s\\n' 'stdout-token'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = write
	defer func() { os.Stderr = old }()

	token, err := ghAuthToken(context.Background())
	os.Stderr = old
	_ = write.Close()
	captured, readErr := io.ReadAll(read)
	_ = read.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if token != "stdout-token" {
		t.Fatalf("token = %q", token)
	}
	if strings.Contains(string(captured), secret) || strings.Contains(token, secret) {
		t.Fatalf("stderr leaked the token: %q", captured)
	}
}
