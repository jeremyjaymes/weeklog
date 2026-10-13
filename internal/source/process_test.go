package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	useHelper(t, "ok", status)
	t.Setenv("WEEKLOG_SECRET_MARK", secret)

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
	useHelper(t, "exit1", status)
	t.Setenv("WEEKLOG_SECRET_MARK", secret)

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
	path := filepath.Join(dir, PluginPrefix+name)
	if err := os.Symlink(bin, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func useHelper(t *testing.T, mode, status string) {
	t.Helper()
	t.Setenv("WEEKLOG_SOURCE_TEST_HELPER", "1")
	t.Setenv("WEEKLOG_HELPER_MODE", mode)
	if status == "" {
		t.Setenv("WEEKLOG_HELPER_STATUS", filepath.Join(t.TempDir(), "unused-status"))
		return
	}
	t.Setenv("WEEKLOG_HELPER_STATUS", status)
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
