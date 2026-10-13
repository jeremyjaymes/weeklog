//go:build unix

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCollectCancelStopsSlowPlugin(t *testing.T) {
	home := isolatePlugins(t)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(home, "plugin.pid")
	script := fmt.Sprintf("#!/bin/sh\necho $$ > %s\nexec /bin/sleep 60\n", strconv.Quote(pidFile))
	if err := os.WriteFile(filepath.Join(bin, "weeklog-source-slow"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "config.toml")
	body := `
[identity]
timezone = "UTC"
week_starts = "monday"

[output]
dir = "/tmp/weeklog-out"

[[source]]
type = "slow"

[[source]]
type = "codex"
`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(home, "weeklog.db")

	root := newRootCmd()
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--config", cfg, "collect", "--db", db, "--since", "2026-10-05", "--until", "2026-10-12", "--timeout", "30s"})
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- root.ExecuteContext(ctx)
	}()

	var pid int
	deadline := time.Now().Add(10 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if pid == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if pid == 0 {
		cancel()
		t.Fatal("plugin did not start")
	}
	cancel()

	var err error
	select {
	case err = <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("collect did not return after cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "source[0] slow: canceled") || strings.Contains(out, "codex") || strings.Contains(out, "link: ok") {
		t.Fatalf("stdout = %s\nstderr = %s", out, stderr.String())
	}
	waitUntilGone(t, pid)
}
