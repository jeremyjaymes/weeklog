//go:build unix

package github

import (
	"os"
	"path/filepath"
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
