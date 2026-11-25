package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

const (
	explainSHA   = "0123456789abcdef0123456789abcdef01234567"
	explainOther = "fedcba9876543210fedcba9876543210fedcba98"
)

func TestExplainLinkUnlinkHelp(t *testing.T) {
	stdout, stderr, err := runWeeklog(t, "explain", "--help")
	if err != nil {
		t.Fatalf("explain help: %v\n%s", err, stderr)
	}
	for _, want := range []string{"work item", "event:ID", "SOURCE/SOURCE_ID", "reason", "--db"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("explain help missing %q\n%s", want, stdout)
		}
	}
	stdout, stderr, err = runWeeklog(t, "link", "--help")
	if err != nil {
		t.Fatalf("link help: %v\n%s", err, stderr)
	}
	for _, want := range []string{"overrides", "pass 3", "override:link", "--week", "--db", "event:ID"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("link help missing %q\n%s", want, stdout)
		}
	}
	stdout, stderr, err = runWeeklog(t, "unlink", "--help")
	if err != nil {
		t.Fatalf("unlink help: %v\n%s", err, stderr)
	}
	for _, want := range []string{"overrides", "pass 3", "override:unlink", "--since", "--until"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("unlink help missing %q\n%s", want, stdout)
		}
	}
}

func TestExplainLinkUnlink(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath, dbPath := writeOverrideConfig(t, home)
	seedOverrideEvents(t, dbPath)

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "explain", "linear/GEN-301", "--db", dbPath)
	if err != nil {
		t.Fatalf("explain before link: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "reason: not linked") || strings.Contains(stdout, "work item") {
		t.Fatalf("stdout = %s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "link-run", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("link-run: %v\n%s\n%s", err, stdout, stderr)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "1", "--db", dbPath)
	if err != nil {
		t.Fatalf("explain item: %v\n%s\n%s", err, stdout, stderr)
	}
	for _, want := range []string{
		"work item 1",
		"title: Webhook retries",
		"status: shipped",
		"linear/GEN-301",
		"reason: ref:GEN-301 ticket",
		"git/" + explainSHA,
		"reason: ref:GEN-301 in commit message",
		"ts: 2026-10-06T12:00:00Z",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("explain item missing %q\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, explainOther) {
		t.Fatalf("item 1 includes the unrelated commit\n%s", stdout)
	}

	id := eventID(t, dbPath, "git", explainOther)
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "event:"+strconv.FormatInt(id, 10), "--db", dbPath)
	if err != nil {
		t.Fatalf("explain event: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "reason: leftover:github.com/example/acme-app 2026-10-09") {
		t.Fatalf("stdout = %s", stdout)
	}
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "git/"+explainOther, "--db", dbPath)
	if err != nil {
		t.Fatalf("explain source: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "git/"+explainOther) || strings.Contains(stdout, "linear/GEN-301") {
		t.Fatalf("stdout = %s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "link", "--week", "2026-W41", "--db", dbPath, "linear/GEN-301", "git/"+explainOther)
	if err != nil {
		t.Fatalf("link: %v\n%s\n%s", err, stdout, stderr)
	}
	for _, want := range []string{
		"db: " + dbPath,
		"override: link linear/GEN-301 git/" + explainOther,
		"window: 2026-W41",
		"link: ok (1 work item)",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("link stdout missing %q\n%s", want, stdout)
		}
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "item:1", "--db", dbPath)
	if err != nil {
		t.Fatalf("explain after link: %v\n%s\n%s", err, stdout, stderr)
	}
	for _, want := range []string{
		"linear/GEN-301",
		"git/" + explainSHA,
		"git/" + explainOther,
		"reason: ref:GEN-301 ticket, override:link",
		"reason: leftover:github.com/example/acme-app 2026-10-09, override:link",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("grouped explain missing %q\n%s", want, stdout)
		}
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Linked by:") || !strings.Contains(stdout, "`override:link`") {
		t.Fatalf("week missing override\n%s", stdout)
	}
	jsonOut, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--json")
	if err != nil {
		t.Fatalf("week json: %v\n%s", err, stderr)
	}
	if !strings.Contains(jsonOut, `"override:link"`) {
		t.Fatalf("json = %s", jsonOut)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "link-run", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("relink: %v\n%s\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "linear/GEN-301", "--db", dbPath)
	if err != nil {
		t.Fatalf("explain after relink: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "git/"+explainOther) || !strings.Contains(stdout, "override:link") {
		t.Fatalf("override did not survive link-run\n%s", stdout)
	}

	restampOverrideEvents(t, dbPath)
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "link-run", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("link after upsert: %v\n%s\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "linear/GEN-301", "--db", dbPath)
	if err != nil {
		t.Fatalf("explain after upsert: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "git/"+explainOther) || !strings.Contains(stdout, "override:link") {
		t.Fatalf("override did not survive re-collect\n%s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "unlink", "--week", "2026-W41", "--db", dbPath, "git/"+explainOther)
	if err != nil {
		t.Fatalf("unlink: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "override: unlink git/"+explainOther) || !strings.Contains(stdout, "link: ok (2 work items)") {
		t.Fatalf("stdout = %s", stdout)
	}
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "git/"+explainOther, "--db", dbPath)
	if err != nil {
		t.Fatalf("explain unlink: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "reason: override:unlink") || strings.Contains(stdout, "linear/GEN-301") {
		t.Fatalf("stdout = %s", stdout)
	}
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "linear/GEN-301", "--db", dbPath)
	if err != nil {
		t.Fatalf("explain ticket: %v\n%s\n%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, explainOther) || !strings.Contains(stdout, "reason: ref:GEN-301 ticket") {
		t.Fatalf("stdout = %s", stdout)
	}

	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath)
	if err != nil {
		t.Fatalf("week after unlink: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "`override:unlink`") || !strings.Contains(stdout, "unrelated notes") {
		t.Fatalf("week = %s", stdout)
	}

	if _, _, err := runWeeklog(t, "--config", cfgPath, "link-run", "--week", "2026-W41", "--db", dbPath); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "git/"+explainOther, "--db", dbPath)
	if err != nil {
		t.Fatalf("explain after second relink: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "reason: override:unlink") || strings.Contains(stdout, "linear/GEN-301") {
		t.Fatalf("unlink did not survive link-run\n%s", stdout)
	}
}

func TestLinkAndExplainRejectBadArgs(t *testing.T) {
	home := isolatePlugins(t)
	cfgPath, dbPath := writeOverrideConfig(t, home)
	missing := filepath.Join(home, "missing.db")

	_, stderr, err := runWeeklog(t, "--config", cfgPath, "explain", "nope", "--db", missing)
	if err == nil || !strings.Contains(err.Error(), "explain") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("bad explain created the database: %v", statErr)
	}

	_, stderr, err = runWeeklog(t, "--config", cfgPath, "link", "git/abc", "git/abc", "--db", missing)
	if err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatalf("self-link created the database: %v", statErr)
	}

	_, stderr, err = runWeeklog(t, "--config", cfgPath, "unlink", "item:1", "--db", missing)
	if err == nil || !strings.Contains(err.Error(), "work item") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}

	seedOverrideEvents(t, dbPath)
	_, stderr, err = runWeeklog(t, "--config", cfgPath, "link", "--db", dbPath, "--week", "2026-W41", "linear/GEN-301", "linear/MISSING")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	_, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "99", "--db", dbPath)
	if err == nil || !strings.Contains(err.Error(), "work item 99") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	_, stderr, err = runWeeklog(t, "--config", cfgPath, "explain", "event:99", "--db", dbPath)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
}

func writeOverrideConfig(t *testing.T, home string) (string, string) {
	t.Helper()
	cfgPath := filepath.Join(home, "config.toml")
	body := `
[identity]
timezone = "UTC"
week_starts = "monday"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, filepath.Join(home, "weeklog.db")
}

func seedOverrideEvents(t *testing.T, dbPath string) {
	t.Helper()
	st := openOverrideDB(t, dbPath)
	defer func() { _ = st.Close() }()
	if _, err := st.UpsertEvents(context.Background(), overrideEvents()); err != nil {
		t.Fatal(err)
	}
}

func restampOverrideEvents(t *testing.T, dbPath string) {
	t.Helper()
	st := openOverrideDB(t, dbPath)
	defer func() { _ = st.Close() }()
	res, err := st.UpsertEvents(context.Background(), overrideEvents())
	if err != nil {
		t.Fatal(err)
	}
	if res.Inserted != 0 || res.Updated != 3 {
		t.Fatalf("upsert = %+v", res)
	}
}

func overrideEvents() []event.Event {
	return []event.Event{
		{
			Source: "linear", SourceID: "GEN-301", Kind: event.KindTicket,
			TS: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), Title: "Webhook retries",
		},
		{
			Source: "git", SourceID: explainSHA, Kind: event.KindCommit,
			TS:    time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC),
			Title: "ship GEN-301", Summary: "ship GEN-301",
			Repo: "github.com/example/acme-app", Branch: "main",
		},
		{
			Source: "git", SourceID: explainOther, Kind: event.KindCommit,
			TS:    time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC),
			Title: "unrelated notes",
			Repo:  "github.com/example/acme-app", Branch: "main",
		},
	}
}

func openOverrideDB(t *testing.T, path string) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func eventID(t *testing.T, dbPath, source, sourceID string) int64 {
	t.Helper()
	st := openOverrideDB(t, dbPath)
	defer func() { _ = st.Close() }()
	rec, err := st.GetEvent(context.Background(), source, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	return rec.ID
}
