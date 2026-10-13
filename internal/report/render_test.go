package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNotesRoundTrip(t *testing.T) {
	rep := Report{
		Week:        "2026-W41",
		Since:       time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:       time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		GeneratedAt: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		Version:     "dev",
		Heading:     "Week 2026-W41 (Oct 5 – Oct 11)",
		Range:       "Oct 5 – Oct 11",
		Summary: Summary{
			Line: "**Shipped 0 · In progress 0 · Explored 0** · ~0m agent time · 0 commits · 0 PRs merged",
		},
		Sections: []Section{},
		Other:    []Other{},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-W41.md")
	if err := WriteMarkdown(path, "", rep); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	notes, ok := ExtractNotes(string(first))
	if !ok || notes != "\n" {
		t.Fatalf("notes = %q ok=%v", notes, ok)
	}
	body := string(first)
	body = strings.Replace(body, NotesMarker+"\n"+NotesMarker, NotesMarker+"\nremember the demo\n"+NotesMarker, 1)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rep.GeneratedAt = rep.GeneratedAt.Add(time.Hour)
	if err := WriteMarkdown(path, "", rep); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(second)
	if !strings.Contains(got, "generated_at: 2026-10-13T13:00:00Z") {
		t.Fatalf("generated_at was not updated\n%s", got)
	}
	if !strings.Contains(got, NotesMarker+"\nremember the demo\n"+NotesMarker) {
		t.Fatalf("notes were not kept\n%s", got)
	}
	if strings.Contains(got, "generated_at: 2026-10-13T12:00:00Z") {
		t.Fatal("old generated_at remains")
	}
	again, err := Markdown(rep, "")
	if err != nil {
		t.Fatal(err)
	}
	rep.Notes = "\nremember the demo\n"
	withNotes, err := Markdown(rep, "")
	if err != nil {
		t.Fatal(err)
	}
	if again == withNotes {
		t.Fatal("notes did not change the render")
	}
	if withNotes != got {
		t.Fatalf("rewrite drifted\n--- file ---\n%s\n--- render ---\n%s", got, withNotes)
	}
}

func TestCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.tmpl")
	const tmpl = "week {{.Week}}\n{{.Summary.Line}}\n"
	if err := os.WriteFile(path, []byte(tmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := Report{
		Week:    "2026-W41",
		Summary: Summary{Line: "hello"},
	}
	got, err := Markdown(rep, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "week 2026-W41\nhello\n") {
		t.Fatalf("custom = %q", got)
	}
	if !strings.Contains(got, NotesMarker) {
		t.Fatalf("custom template dropped notes: %q", got)
	}
	if _, err := Markdown(rep, filepath.Join(dir, "missing.tmpl")); err == nil {
		t.Fatal("expected a missing template to fail")
	}
	bad := filepath.Join(dir, "bad.tmpl")
	if err := os.WriteFile(bad, []byte("{{.Missing}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Markdown(rep, bad); err == nil {
		t.Fatal("expected an unknown field to fail")
	}
}

func TestJSONOmitsNotes(t *testing.T) {
	rep := Report{
		Week:     "2026-W41",
		Heading:  "Week 2026-W41 (Oct 5 – Oct 11)",
		Notes:    "\nsecret note\n",
		Sections: nil,
		Other:    nil,
	}
	data, err := JSON(rep)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "secret note") || strings.Contains(got, "notes") {
		t.Fatalf("json leaked notes: %s", got)
	}
	if !strings.Contains(got, `"sections": []`) || !strings.Contains(got, `"other": []`) {
		t.Fatalf("json = %s", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatal("json missing trailing newline")
	}
}
