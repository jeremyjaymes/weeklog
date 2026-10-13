//go:build unix

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCanceledRootContextStopsSlowPlugin(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"sources", "--timeout", "30s"})

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
		select {
		case err := <-errCh:
			t.Fatalf("plugin did not start: %v\n%s\n%s", err, stdout.String(), stderr.String())
		case <-time.After(5 * time.Second):
			t.Fatal("plugin did not start and sources did not return")
		}
	}

	cancel()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("sources did not return after cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	waitUntilGone(t, pid)
}

func waitUntilGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin pid %d still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
