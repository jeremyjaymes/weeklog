package source

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

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestProcessCollectRoundTrip(t *testing.T) {
	const secret = "wksec_STDIN_only_91aa"
	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	status := filepath.Join(dir, "status")
	t.Setenv("WEEKLOG_SECRET_MARK", secret)
	useHelper(t, "ok", status)

	cursor := "prev"
	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	var got []event.Event
	result, err := proc.Collect(context.Background(), CollectRequest{
		Since:    time.Date(2026, 10, 5, 0, 0, 0, 0, time.FixedZone("CDT", -5*3600)),
		Until:    time.Date(2026, 10, 12, 0, 0, 0, 0, time.FixedZone("CDT", -5*3600)),
		Cursor:   &cursor,
		Config:   map[string]any{"token": secret, "account_id": "123"},
		Identity: Identity{Emails: []string{"jeremy@example.com"}, Handles: map[string]string{"github": "jeremyjaymes"}},
	}, func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Source != "todoist" || got[0].Title != "Ship helper" || got[0].Kind != event.KindTicket {
		t.Fatalf("events = %#v", got)
	}
	if !result.HasCursor || result.Cursor != "cur-1" || result.Dropped != 0 || result.Events != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Logs) != 1 || result.Logs[0].Level != "warn" {
		t.Fatalf("logs = %#v", result.Logs)
	}
	body := readStatus(t, status, secret)
	for _, want := range []string{"argc=2\n", "arg1=collect\n", "saw_secret=yes\n", "cursor=prev\n", "protocol=1\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status missing %q\n%s", want, body)
		}
	}
}

func TestProcessDoesNotPutSecretInArgvOrErrors(t *testing.T) {
	const secret = "wksec_ARGV_must_miss_44bb"
	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	status := filepath.Join(dir, "status")
	t.Setenv("WEEKLOG_SECRET_MARK", secret)
	useHelper(t, "exit1", status)

	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	_, err := proc.Describe(context.Background())
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("describe error contained the secret")
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "exit 1") {
		t.Fatalf("err = %v", err)
	}
	body := readStatus(t, status, secret)
	if !strings.Contains(body, "arg1=describe\n") || !strings.Contains(body, "saw_secret=no\n") {
		t.Fatalf("status = %s", body)
	}

	useHelper(t, "exit2", filepath.Join(dir, "status2"))
	_, err = proc.Collect(context.Background(), sampleRequest(secret), nil)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("collect err = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("collect error contained the secret")
	}
	if !strings.Contains(err.Error(), "missing token") || !strings.Contains(err.Error(), "exit 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestProcessDropsBadLines(t *testing.T) {
	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	useHelper(t, "badlines", "")
	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	var got []event.Event
	result, err := proc.Collect(context.Background(), sampleRequest("unused"), func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SourceID != "e1" || got[1].SourceID != "e3" || got[1].Source != "todoist" {
		t.Fatalf("events = %#v", got)
	}
	if result.Dropped < 3 || result.Cursor != "cur-1" || len(result.Logs) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestProcessSkipsOversizedLine(t *testing.T) {
	orig := maxMessageBytes
	maxMessageBytes = 200
	t.Cleanup(func() { maxMessageBytes = orig })

	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	useHelper(t, "hugeline", "")
	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	var got []event.Event
	result, err := proc.Collect(context.Background(), sampleRequest("unused"), func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Dropped != 1 || len(got) != 1 || got[0].Source != "todoist" {
		t.Fatalf("dropped=%d events=%#v", result.Dropped, got)
	}
}

func TestProcessTimeout(t *testing.T) {
	dir := t.TempDir()
	bin := linkHelper(t, dir, "slow")
	useHelper(t, "sleep", "")
	proc := &Process{Name: "slow", Path: bin, Timeout: time.Second}
	start := time.Now()
	_, err := proc.Describe(context.Background())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}
}

func TestProcessProtocolFailures(t *testing.T) {
	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	for _, mode := range []string{"badjson", "protocol", "name"} {
		t.Run(mode, func(t *testing.T) {
			useHelper(t, mode, "")
			_, err := proc.Describe(context.Background())
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestProcessRejectsBadRequestBeforeLaunch(t *testing.T) {
	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	marker := filepath.Join(dir, "launched")
	useHelper(t, "ok", marker)
	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	_, err := proc.Collect(context.Background(), CollectRequest{Protocol: 2}, nil)
	if !errors.Is(err, ErrConfig) {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("plugin launched for an invalid request")
	}
}

func TestServeFixtureMatchesInProcess(t *testing.T) {
	dir := t.TempDir()
	bin := linkHelper(t, dir, "fixture")
	useHelper(t, "serve-fixture", "")
	proc := &Process{Name: "fixture", Path: bin, Timeout: 5 * time.Second}
	manifest, err := proc.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	direct, err := (fixtureSource{name: "fixture"}).Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != direct.Name || manifest.Version != direct.Version || manifest.Config["token"].Type != TypeSecret {
		t.Fatalf("manifest = %#v", manifest)
	}
	cursor := "prev"
	var got []event.Event
	result, err := proc.Collect(context.Background(), sampleRequestWithCursor("tok", &cursor), func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SourceID != "e1" || result.Cursor != "next-prev" {
		t.Fatalf("events=%#v result=%#v", got, result)
	}
	if len(result.Logs) != 1 || result.Logs[0].Msg != "skipped 2 archived projects" {
		t.Fatalf("logs = %#v", result.Logs)
	}
}

func TestProcessMissingBinary(t *testing.T) {
	proc := &Process{Name: "missing", Path: filepath.Join(t.TempDir(), "absent"), Timeout: time.Second}
	_, err := proc.Describe(context.Background())
	if !errors.Is(err, ErrFailed) {
		t.Fatal(err)
	}
}

func linkHelper(t *testing.T, dir, name string) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(dir, name+"-control")
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	// The plugin process does not inherit the test's environment, so the
	// wrapper reads the helper controls from disk and sets them itself.
	t.Setenv("WEEKLOG_TEST_HELPER_CONTROL", control)
	path := filepath.Join(dir, PluginPrefix+name)
	script := fmt.Sprintf(`#!/bin/sh
set -eu
control=%s
mode=$(cat "$control/mode")
status=$(cat "$control/status")
secret=""
if [ -f "$control/secret" ]; then
  secret=$(cat "$control/secret")
fi
export WEEKLOG_SOURCE_TEST_HELPER=1
export WEEKLOG_HELPER_MODE="$mode"
export WEEKLOG_HELPER_STATUS="$status"
export WEEKLOG_HELPER_NAME=%s
if [ -n "$secret" ]; then
  export WEEKLOG_SECRET_MARK="$secret"
fi
exec %s "$@"
`, strconv.Quote(control), strconv.Quote(name), strconv.Quote(bin))
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func useHelper(t *testing.T, mode, status string) {
	t.Helper()
	control := os.Getenv("WEEKLOG_TEST_HELPER_CONTROL")
	if control == "" {
		t.Fatal("linkHelper was not called")
	}
	if status == "" {
		status = filepath.Join(t.TempDir(), "unused-status")
	}
	writeControl(t, filepath.Join(control, "mode"), mode)
	writeControl(t, filepath.Join(control, "status"), status)
	if secret := os.Getenv("WEEKLOG_SECRET_MARK"); secret != "" {
		writeControl(t, filepath.Join(control, "secret"), secret)
	}
}

func writeControl(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProcessStripsParentEnvironment(t *testing.T) {
	const token = "weeklog-fake-token-should-not-leak"
	t.Setenv("WEEKLOG_FAKE_TOKEN", token)
	t.Setenv("LC_ALL", "C")
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(t.TempDir(), "run"))
	if os.Getenv("HOME") == "" {
		t.Setenv("HOME", t.TempDir())
	}
	if os.Getenv("PATH") == "" {
		t.Fatal("PATH is empty")
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")
	script := fmt.Sprintf("#!/bin/sh\nset > %s\nprintf '%%s\\n' '%s'\n",
		strconv.Quote(out),
		`{"protocol":1,"name":"envplug","version":"0.1.0","config":{}}`)
	path := filepath.Join(dir, PluginPrefix+"envplug")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	proc := &Process{Name: "envplug", Path: path, Timeout: 5 * time.Second}
	if _, err := proc.Describe(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, token) || strings.Contains(text, "WEEKLOG_FAKE_TOKEN=") {
		t.Fatalf("plugin environment leaked the token:\n%s", text)
	}
	if !strings.Contains(text, "PATH=") || !strings.Contains(text, "HOME=") {
		t.Fatalf("plugin environment = %s", text)
	}
	if !strings.Contains(text, "LC_ALL=") || !strings.Contains(text, "XDG_RUNTIME_DIR=") {
		t.Fatalf("plugin environment dropped allowlisted vars:\n%s", text)
	}
}

func TestProcessRedactsStderr(t *testing.T) {
	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	useHelper(t, "stderr-token", "")
	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	_, err := proc.Describe(context.Background())
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(failure.Stderr, helperStderrToken) || strings.Contains(err.Error(), helperStderrToken) {
		t.Fatalf("stderr leaked the token: %q", failure.Stderr)
	}
	if !strings.Contains(failure.Stderr, "[REDACTED:") {
		t.Fatalf("stderr = %q", failure.Stderr)
	}
}

func TestProcessCollectEventCap(t *testing.T) {
	orig := maxCollectEvents
	maxCollectEvents = 2
	t.Cleanup(func() { maxCollectEvents = orig })

	dir := t.TempDir()
	bin := linkHelper(t, dir, "todoist")
	useHelper(t, "many", "")
	proc := &Process{Name: "todoist", Path: bin, Timeout: 5 * time.Second}
	_, err := proc.Collect(context.Background(), sampleRequest("unused"), nil)
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "exceeded") || !strings.Contains(err.Error(), "2 events") {
		t.Fatalf("err = %v", err)
	}
}

func sampleRequest(secret string) CollectRequest {
	return sampleRequestWithCursor(secret, nil)
}

func sampleRequestWithCursor(secret string, cursor *string) CollectRequest {
	return CollectRequest{
		Since:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Cursor: cursor,
		Config: map[string]any{"token": secret},
	}
}

func readStatus(t *testing.T, path, secret string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" && strings.Contains(string(data), secret) {
		t.Fatal("status file contained the secret")
	}
	return string(data)
}
