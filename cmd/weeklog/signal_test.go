package main

import (
	"context"
	"strings"
	"testing"
)

func TestTimeoutMustBePositive(t *testing.T) {
	_, _, err := runWeeklog(t, "sources", "--timeout", "0")
	if err == nil || !strings.Contains(err.Error(), "--timeout must be positive, got 0s") {
		t.Fatalf("sources err = %v", err)
	}
	_, _, err = runWeeklog(t, "doctor", "--timeout", "-1s")
	if err == nil || !strings.Contains(err.Error(), "--timeout must be positive, got -1s") {
		t.Fatalf("doctor err = %v", err)
	}
}

func TestNewSourceRegistry(t *testing.T) {
	// newSourceRegistry panics if two built-ins share a name. Constructing it
	// here makes that failure show up in CI.
	reg := newSourceRegistry()
	reg.SetPath("")
	reg.SetPluginDir(t.TempDir())
	list, err := reg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Sources) != len(builtins) {
		t.Fatalf("sources = %d, want %d", len(list.Sources), len(builtins))
	}
	for i, item := range builtins {
		if list.Sources[i].Name != item.name || list.Sources[i].Err != nil {
			t.Fatalf("source %d = %#v", i, list.Sources[i])
		}
	}
}
