package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/config"
	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/redact"
)

func loadSamples(t *testing.T) []redact.Sample {
	t.Helper()
	samples, err := redact.LoadSamples(filepath.Join("..", "..", "testdata", "secrets", "corpus.json"))
	if err != nil {
		t.Fatalf("LoadSamples: %v", err)
	}
	return samples
}

func hide(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[canary]")
}

func TestSecretCorpusNotInDatabaseOrMarkdown(t *testing.T) {
	samples := loadSamples(t)
	secrets := make([]string, 0, 1)
	for _, sample := range samples {
		if sample.NeedsKey {
			secrets = append(secrets, sample.Secret)
		}
	}
	r, err := redact.New(redact.Options{Secrets: secrets})
	if err != nil {
		t.Fatal(err)
	}

	s, path := openFile(t)
	s.SetRedactor(r)

	fields := []string{
		"source", "source_id", "kind", "actor", "title", "summary",
		"url", "repo", "branch", "cwd", "files", "refs", "state",
		"metrics", "body_ref",
	}
	events := make([]event.Event, 0, len(samples)*len(fields))
	n := 0
	for _, sample := range samples {
		payload := "before " + sample.Text + " after"
		for _, field := range fields {
			ev := event.Event{
				Source:   "corpus",
				SourceID: fmt.Sprintf("row/%d", n),
				Kind:     event.KindCommit,
				TS:       time.Date(2026, 10, 8, 12, 0, n%60, 0, time.UTC),
				Title:    "weekly note",
				State:    event.StateOpen,
			}
			injectSecret(&ev, field, payload, n)
			events = append(events, ev)
			n++
		}
	}

	ctx := context.Background()
	if _, err := s.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	count := mustCount(t, s)
	if count != len(events) {
		t.Fatalf("count = %d, want %d", count, len(events))
	}
	if _, err := s.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("second UpsertEvents: %v", err)
	}
	if again := mustCount(t, s); again != count {
		t.Fatalf("duplicate collect count = %d, want %d", again, count)
	}

	dump := dumpDatabase(t, s)
	rendered := renderStored(t, s)
	raw := readStoreBytes(t, s, path)
	for _, sample := range samples {
		assertAbsent(t, "sqlite", dump, sample)
		assertAbsent(t, "markdown", rendered, sample)
		assertAbsent(t, "dbfile", raw, sample)
		for _, keep := range sample.Keep {
			if !strings.Contains(dump, keep) || !strings.Contains(rendered, keep) {
				t.Fatalf("%s: %q did not survive in stored text", sample.Name, keep)
			}
		}
	}
	if !strings.Contains(rendered, "## ") {
		t.Fatal("rendered markdown has no headings")
	}
}

func TestSummarySecretIsRedactedBeforeTruncate(t *testing.T) {
	secret := "ghp_" + strings.Repeat("q", 36)
	s, path := openFile(t)
	ev := planEvent()
	ev.Summary = strings.Repeat("x", 490) + secret
	if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.GetEvent(context.Background(), ev.Source, ev.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Summary, "ghp_") || strings.Contains(rec.Summary, "qqqqqqqq") {
		t.Fatalf("summary leaked a secret prefix: %q", rec.Summary)
	}
	if utfLen(rec.Summary) > 500 {
		t.Fatalf("summary length %d", utfLen(rec.Summary))
	}
	raw := readStoreBytes(t, s, path)
	if strings.Contains(raw, secret) || strings.Contains(raw, "ghp_qqqq") {
		t.Fatal("database file contains the summary secret")
	}
}

func TestInvalidEventDoesNotStoreSecret(t *testing.T) {
	secret := "ghp_" + strings.Repeat("q", 36)
	s, path := openFile(t)
	ev := event.Event{
		Source:   " ",
		SourceID: "id-1",
		Kind:     event.KindCommit,
		TS:       time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Title:    secret,
	}
	if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err == nil {
		t.Fatal("expected validation to fail")
	}
	if strings.Contains(readStoreBytes(t, s, path), "ghp_") {
		t.Fatal("failed upsert wrote the secret")
	}
	if ev.Title != secret {
		t.Fatal("input title was mutated")
	}
}

func TestUpsertDoesNotMutateCaller(t *testing.T) {
	secret := "ghp_" + strings.Repeat("w", 36)
	s := openMem(t)
	ev := planEvent()
	ev.Title = "before " + secret + " after"
	ev.Files = []string{"app/" + secret + ".go"}
	title := ev.Title
	file := ev.Files[0]
	if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err != nil {
		t.Fatal(err)
	}
	if ev.Title != title || ev.Files[0] != file {
		t.Fatal("upsert mutated the caller")
	}
	rec, err := s.GetEvent(context.Background(), ev.Source, ev.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Title, secret) || strings.Contains(strings.Join(rec.Files, " "), secret) {
		t.Fatal("stored event kept the secret")
	}
	if !strings.Contains(rec.Title, "[REDACTED:github-token]") {
		t.Fatalf("title = %q", rec.Title)
	}
}

func TestSetRedactorNilRestoresBuiltin(t *testing.T) {
	secret := "ghp_" + strings.Repeat("u", 36)
	allowed, err := redact.New(redact.Options{Allow: []string{secret}})
	if err != nil {
		t.Fatal(err)
	}
	s := openMem(t)
	s.SetRedactor(allowed)
	ev := planEvent()
	ev.Title = secret
	if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.GetEvent(context.Background(), ev.Source, ev.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Title != secret {
		t.Fatalf("allow list did not apply: %q", rec.Title)
	}

	s.SetRedactor(nil)
	ev.SourceID = ev.SourceID + "-2"
	if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err != nil {
		t.Fatal(err)
	}
	rec, err = s.GetEvent(context.Background(), ev.Source, ev.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Title, secret) || rec.Title != "[REDACTED:github-token]" {
		t.Fatalf("default redactor title = %q", rec.Title)
	}
}

func TestConfiguredRedactionOnWrite(t *testing.T) {
	const secret = "weeklog-cfg-9f3a2c1b"
	allowed := "ABCDEFGHIJKLMNOPQRSTUVWX"
	banned := "ZYXWVUTSRQPONMLKJIHGFEDCBA"
	custom := "WKCUST-654321"
	token := "ghp_" + strings.Repeat("k", 36)

	t.Setenv("WEEKLOG_CORPUS_SECRET", secret)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	body := `
[output]
dir = "/tmp/weeklog-out"

[redaction]
extra_patterns = ["WKCUST-[0-9]{6}"]
allow = ["ABCDEFGHIJKLMNOPQRSTUVWX"]

[[source]]
type = "linear"
token = "env:WEEKLOG_CORPUS_SECRET"
`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := config.Check(context.Background(), cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() {
		t.Fatalf("config check: %s", result.Report())
	}
	values := result.SecretValues()
	if len(values) != 1 || values[0] != secret {
		t.Fatal("SecretValues mismatch")
	}
	r, err := redact.New(redact.Options{
		ExtraPatterns: result.Config.Redaction.ExtraPatterns,
		Allow:         result.Config.Redaction.Allow,
		Secrets:       values,
	})
	if err != nil {
		t.Fatal(err)
	}

	s, path := openFile(t)
	s.SetRedactor(r)
	ev := planEvent()
	ev.Title = strings.Join([]string{"note", secret, custom, token, allowed, banned}, " ")
	if _, err := s.UpsertEvents(context.Background(), []event.Event{ev}); err != nil {
		t.Fatal(err)
	}
	rec, err := s.GetEvent(context.Background(), ev.Source, ev.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	raw := readStoreBytes(t, s, path)
	rendered := renderRecord(rec)
	for _, leak := range []string{secret, custom, token, banned} {
		if strings.Contains(rec.Title, leak) || strings.Contains(rendered, leak) || strings.Contains(raw, leak) {
			t.Fatalf("leaked %s into %s", "configured value", hide(rec.Title+"\n"+rendered, leak))
		}
	}
	if !strings.Contains(rec.Title, allowed) || !strings.Contains(rendered, allowed) {
		t.Fatalf("allow entry was removed: %q", rec.Title)
	}
}

func injectSecret(ev *event.Event, field, payload string, n int) {
	switch field {
	case "source":
		ev.Source = fmt.Sprintf("src-%d-%s", n, payload)
	case "source_id":
		ev.SourceID = fmt.Sprintf("id-%d-%s", n, payload)
	case "kind":
		ev.Kind = event.Kind(payload)
	case "actor":
		ev.Actor = payload
	case "title":
		ev.Title = payload
	case "summary":
		ev.Summary = payload
	case "url":
		ev.URL = payload
	case "repo":
		ev.Repo = payload
	case "branch":
		ev.Branch = payload
	case "cwd":
		ev.CWD = payload
	case "files":
		ev.Files = []string{payload}
	case "refs":
		ev.Refs = []string{payload}
	case "state":
		ev.State = event.State(payload)
	case "metrics":
		ev.Metrics = map[string]float64{payload: 1}
	case "body_ref":
		ev.BodyRef = payload
	default:
		panic("unknown field " + field)
	}
}

func assertAbsent(t *testing.T, where string, haystack string, sample redact.Sample) {
	t.Helper()
	bans := append([]string{sample.Secret}, sample.AlsoBan...)
	if mid := middleSlice(sample.Secret); mid != "" {
		bans = append(bans, mid)
	}
	for _, ban := range bans {
		if ban == "" {
			continue
		}
		if strings.Contains(haystack, ban) {
			t.Fatalf("%s: %s leaked into %s: %s", sample.Name, "secret", where, hide(snippet(haystack, ban), ban))
		}
	}
}

func middleSlice(secret string) string {
	if len(secret) < 12 {
		return ""
	}
	mid := len(secret) / 2
	return secret[mid-4 : mid+4]
}

func snippet(haystack, needle string) string {
	i := strings.Index(haystack, needle)
	if i < 0 {
		return ""
	}
	start := i - 40
	if start < 0 {
		start = 0
	}
	end := i + len(needle) + 40
	if end > len(haystack) {
		end = len(haystack)
	}
	return haystack[start:end]
}

func renderStored(t *testing.T, s *Store) string {
	t.Helper()
	recs, err := s.ListEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, rec := range recs {
		b.WriteString(renderRecord(rec))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderRecord(rec Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", rec.Title)
	fmt.Fprintf(&b, "%s\n", rec.Summary)
	fmt.Fprintf(&b, "- %s/%s %s %s\n", rec.Source, rec.SourceID, rec.Kind, rec.State)
	fmt.Fprintf(&b, "- %s %s %s %s\n", rec.Actor, rec.URL, rec.Repo, rec.Branch)
	fmt.Fprintf(&b, "- %s %s\n", rec.CWD, rec.BodyRef)
	for _, file := range rec.Files {
		fmt.Fprintf(&b, "- file %s\n", file)
	}
	for _, ref := range rec.Refs {
		fmt.Fprintf(&b, "- ref %s\n", ref)
	}
	for key, value := range rec.Metrics {
		fmt.Fprintf(&b, "- metric %s=%v\n", key, value)
	}
	return b.String()
}

func dumpDatabase(t *testing.T, s *Store) string {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	for _, table := range tables {
		if strings.IndexFunc(table, func(r rune) bool {
			return (r < 'a' || r > 'z') && r != '_'
		}) >= 0 {
			t.Fatalf("unexpected table %q", table)
		}
		tableRows, err := s.db.Query(`SELECT * FROM ` + table)
		if err != nil {
			t.Fatal(err)
		}
		if err := appendRows(&b, tableRows); err != nil {
			_ = tableRows.Close()
			t.Fatal(err)
		}
		if err := tableRows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

func appendRows(b *strings.Builder, rows *sql.Rows) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		dest := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for _, value := range dest {
			switch v := value.(type) {
			case nil:
			case string:
				b.WriteString(v)
			case []byte:
				b.Write(v)
			default:
				fmt.Fprint(b, v)
			}
			b.WriteByte('\n')
		}
	}
	return rows.Err()
}

func readStoreBytes(t *testing.T, s *Store, path string) string {
	t.Helper()
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

func utfLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
