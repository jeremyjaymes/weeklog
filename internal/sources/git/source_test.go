package git

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

func TestLookupRemoteDashName(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, nil, "init", "-b", "main")
	runGit(t, repo, nil, "remote", "add", "--", "--push", "https://github.com/acme/dash.git")

	got, err := lookupRemote(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://github.com/acme/dash.git" {
		t.Fatalf("remote = %q", got)
	}
}

func TestCollectTempRepo(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "org", "otto")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, nil, "init", "-b", "main")
	runGit(t, repo, nil, "remote", "add", "origin", "git@github.com:JeremyJaymes/otto.git")

	writeRepoFile(t, filepath.Join(repo, "old.txt"), "old\n")
	runGit(t, repo, nil, "add", "old.txt")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-09-01T12:00:00Z", "Old work")

	writeRepoFile(t, filepath.Join(repo, "README.md"), "hello\n")
	runGit(t, repo, nil, "add", "README.md")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-05T00:00:00Z", "Start GEN-100\n\nEncode as UTF-8")

	writeRepoFile(t, filepath.Join(repo, "bob.txt"), "bob\n")
	runGit(t, repo, nil, "add", "bob.txt")
	commitAt(t, repo, "Bob", "bob@example.com", "2026-10-06T12:00:00Z", "Bob only")

	writeRepoFile(t, filepath.Join(repo, "note.txt"), "note\n")
	runGit(t, repo, nil, "add", "note.txt")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-07T10:00:00Z", "Add note\n\nSee a1b2c3d")

	runGit(t, repo, authorEnv("Alice", "alice@example.com", "2026-10-07T12:00:00Z"), "revert", "--no-edit", "HEAD")

	runGit(t, repo, nil, "checkout", "-b", "feature")
	writeRepoFile(t, filepath.Join(repo, "app", "retry.go"), "retry\n")
	writeRepoFile(t, filepath.Join(repo, "README.md"), "hello\nmore\n")
	runGit(t, repo, nil, "add", "app/retry.go", "README.md")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-08T10:00:00Z", "Handle retries #12\n\nFixes GEN-301\nSee a1b2c3d")
	runGit(t, repo, nil, "mv", "app/retry.go", "app/retries.go")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-08T12:00:00Z", "Rename retry file")

	runGit(t, repo, nil, "checkout", "main")
	runGit(t, repo, nil, "checkout", "-b", "spike")
	writeRepoFile(t, filepath.Join(repo, "spike.txt"), "spike\n")
	runGit(t, repo, nil, "add", "spike.txt")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-11T09:00:00Z", "Try Lob adapter")

	runGit(t, repo, nil, "checkout", "main")
	runGit(t, repo, authorEnv("Alice", "alice@example.com", "2026-10-10T15:00:00Z"), "merge", "--no-ff", "feature", "-m", "Merge branch 'feature'")

	writeRepoFile(t, filepath.Join(repo, "late.txt"), "late\n")
	runGit(t, repo, nil, "add", "late.txt")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-12T00:00:00Z", "Too late")

	edge := filepath.Join(root, "edge", "one", "two")
	if err := os.MkdirAll(edge, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, edge, nil, "init", "-b", "main")
	runGit(t, edge, nil, "remote", "add", "origin", "https://user:s3cret-token@github.com/acme/edge.git")
	writeRepoFile(t, filepath.Join(edge, "edge.txt"), "edge\n")
	runGit(t, edge, nil, "add", "edge.txt")
	commitAt(t, edge, "Alice", "alice@example.com", "2026-10-09T08:00:00Z", "Edge marker")

	deep := filepath.Join(root, "skip", "one", "two", "three")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, deep, nil, "init", "-b", "main")
	writeRepoFile(t, filepath.Join(deep, "deep.txt"), "deep\n")
	runGit(t, deep, nil, "add", "deep.txt")
	commitAt(t, deep, "Alice", "alice@example.com", "2026-10-09T08:00:00Z", "Too deep")

	nested := filepath.Join(repo, "vendor", "lib")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, nested, nil, "init", "-b", "main")
	writeRepoFile(t, filepath.Join(nested, "lib.txt"), "lib\n")
	runGit(t, nested, nil, "add", "lib.txt")
	commitAt(t, nested, "Alice", "alice@example.com", "2026-10-09T08:00:00Z", "Nested vendor")

	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	missing := filepath.Join(root, "missing")
	var got []event.Event
	result, err := New().Collect(context.Background(), source.CollectRequest{
		Protocol: source.ProtocolVersion,
		Since:    since,
		Until:    until,
		Config: map[string]any{
			"roots":   []string{root, missing},
			"authors": []string{"Alice@Example.com"},
		},
		Identity: source.Identity{Emails: []string{"bob@example.com"}},
	}, func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if result.Events != len(got) {
		t.Fatalf("Events = %d, emitted %d", result.Events, len(got))
	}
	if !logContains(result.Logs, "root not found: "+missing) {
		t.Fatalf("logs = %#v", result.Logs)
	}

	byTitle := map[string]event.Event{}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "s3cret-token") {
		t.Fatalf("remote credential leaked: %s", blob)
	}
	for _, ev := range got {
		if ev.Source != "git" || ev.Kind != event.KindCommit {
			t.Fatalf("event = %#v", ev)
		}
		if len(ev.SourceID) != 40 || strings.Trim(ev.SourceID, "0123456789abcdef") != "" {
			t.Fatalf("source_id = %q", ev.SourceID)
		}
		if _, ok := byTitle[ev.Title]; ok {
			t.Fatalf("duplicate title %q", ev.Title)
		}
		byTitle[ev.Title] = ev
		switch ev.Title {
		case "Old work", "Bob only", "Too late", "Too deep", "Nested vendor":
			t.Fatalf("unexpected event %q", ev.Title)
		}
	}

	wantTitles := []string{
		"Start GEN-100",
		"Add note",
		`Revert "Add note"`,
		"Handle retries #12",
		"Rename retry file",
		"Edge marker",
		"Merge branch 'feature'",
		"Try Lob adapter",
	}
	if len(got) != len(wantTitles) {
		titles := make([]string, len(got))
		for i, ev := range got {
			titles[i] = ev.Title + " branch=" + ev.Branch
		}
		t.Fatalf("events = %v", titles)
	}
	for i, title := range wantTitles {
		if got[i].Title != title {
			t.Fatalf("event %d = %q, want %q", i, got[i].Title, title)
		}
	}

	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	edgeAbs, err := filepath.Abs(edge)
	if err != nil {
		t.Fatal(err)
	}

	start := byTitle["Start GEN-100"]
	if !start.TS.Equal(since) {
		t.Fatalf("start ts = %s", start.TS)
	}
	assertCommit(t, start, repoAbs, "github.com/JeremyJaymes/otto", "alice@example.com", []string{"README.md"}, []string{"GEN-100"}, 1, 0)
	if strings.Join(start.Refs, ",") != "GEN-100" {
		t.Fatalf("refs = %#v", start.Refs)
	}
	if !strings.Contains(start.Summary, "Encode as UTF-8") {
		t.Fatalf("summary = %q", start.Summary)
	}

	note := byTitle["Add note"]
	assertCommit(t, note, repoAbs, "github.com/JeremyJaymes/otto", "alice@example.com", []string{"note.txt"}, []string{"a1b2c3d"}, 1, 0)

	revert := byTitle[`Revert "Add note"`]
	assertCommit(t, revert, repoAbs, "github.com/JeremyJaymes/otto", "alice@example.com", []string{"note.txt"}, []string{note.SourceID}, 0, 1)
	if revert.URL != "https://github.com/JeremyJaymes/otto/commit/"+revert.SourceID {
		t.Fatalf("url = %s", revert.URL)
	}

	retries := byTitle["Handle retries #12"]
	assertCommit(t, retries, repoAbs, "github.com/JeremyJaymes/otto", "alice@example.com", nil, []string{"#12", "GEN-301", "a1b2c3d"}, 2, 0)
	if !sameStrings(retries.Files, []string{"README.md", "app/retry.go"}) && !sameStrings(retries.Files, []string{"app/retry.go", "README.md"}) {
		t.Fatalf("files = %#v", retries.Files)
	}

	renamed := byTitle["Rename retry file"]
	assertCommit(t, renamed, repoAbs, "github.com/JeremyJaymes/otto", "alice@example.com", []string{"app/retries.go"}, nil, 0, 0)

	edgeEv := byTitle["Edge marker"]
	assertCommit(t, edgeEv, edgeAbs, "github.com/acme/edge", "alice@example.com", []string{"edge.txt"}, nil, 1, 0)
	if edgeEv.URL != "https://github.com/acme/edge/commit/"+edgeEv.SourceID {
		t.Fatalf("edge url = %s", edgeEv.URL)
	}

	merged := byTitle["Merge branch 'feature'"]
	if merged.Branch != "main" || merged.CWD != repoAbs || merged.Repo != "github.com/JeremyJaymes/otto" {
		t.Fatalf("merge = %#v", merged)
	}
	spike := byTitle["Try Lob adapter"]
	if spike.Branch != "spike" || spike.CWD != repoAbs {
		t.Fatalf("spike = %#v", spike)
	}
	if renamed.Branch != "feature" {
		t.Fatalf("rename branch = %q", renamed.Branch)
	}
}

func TestCollectAuthorSelection(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, nil, "init", "-b", "main")
	writeRepoFile(t, filepath.Join(repo, "a.txt"), "a\n")
	runGit(t, repo, nil, "add", "a.txt")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-06T00:00:00Z", "Alice commit")
	writeRepoFile(t, filepath.Join(repo, "b.txt"), "b\n")
	runGit(t, repo, nil, "add", "b.txt")
	commitAt(t, repo, "Bob", "bob@example.com", "2026-10-07T00:00:00Z", "Bob commit")

	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	collect := func(authors []string, includeAuthors bool, emails []string) []string {
		t.Helper()
		cfg := map[string]any{"roots": []string{repo}}
		if includeAuthors {
			cfg["authors"] = authors
		}
		var titles []string
		_, err := New().Collect(context.Background(), source.CollectRequest{
			Since:    since,
			Until:    until,
			Config:   cfg,
			Identity: source.Identity{Emails: emails},
		}, func(ev event.Event) error {
			titles = append(titles, ev.Title)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return titles
	}
	if got := collect([]string{"Alice <alice@example.com>"}, true, []string{"bob@example.com"}); !sameStrings(got, []string{"Alice commit"}) {
		t.Fatalf("email filter = %#v", got)
	}
	if got := collect(nil, false, []string{"bob@example.com"}); !sameStrings(got, []string{"Bob commit"}) {
		t.Fatalf("identity filter = %#v", got)
	}
	if got := collect([]string{}, true, []string{"alice@example.com"}); !sameStrings(got, []string{"Alice commit", "Bob commit"}) {
		t.Fatalf("empty authors = %#v", got)
	}
	if got := collect([]string{"alice"}, true, nil); !sameStrings(got, []string{"Alice commit"}) {
		t.Fatalf("name filter = %#v", got)
	}
}

func TestCollectHomeAndDuplicateRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := filepath.Join(home, "proj")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, nil, "init", "-b", "main")
	writeRepoFile(t, filepath.Join(repo, "a.txt"), "a\n")
	runGit(t, repo, nil, "add", "a.txt")
	commitAt(t, repo, "Alice", "alice@example.com", "2026-10-06T00:00:00Z", "Home commit")

	var got []event.Event
	_, err := New().Collect(context.Background(), source.CollectRequest{
		Since: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Config: map[string]any{
			"roots": []any{"~/proj", repo},
		},
		Identity: source.Identity{Emails: []string{"alice@example.com"}},
	}, func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Home commit" {
		t.Fatalf("events = %#v", got)
	}
}

func TestCollectEmptyRepo(t *testing.T) {
	root := t.TempDir()
	empty := filepath.Join(root, "empty")
	used := filepath.Join(root, "used")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(used, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, empty, nil, "init", "-b", "main")
	runGit(t, used, nil, "init", "-b", "main")
	writeRepoFile(t, filepath.Join(used, "a.txt"), "a\n")
	runGit(t, used, nil, "add", "a.txt")
	commitAt(t, used, "Alice", "alice@example.com", "2026-10-06T00:00:00Z", "Kept commit")

	var titles []string
	_, err := New().Collect(context.Background(), source.CollectRequest{
		Since:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Config: map[string]any{"roots": []string{root}},
	}, func(ev event.Event) error {
		titles = append(titles, ev.Title)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(titles, []string{"Kept commit"}) {
		t.Fatalf("titles = %#v", titles)
	}
}

func TestCollectRejectsBadConfig(t *testing.T) {
	since := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		config map[string]any
		since  time.Time
		until  time.Time
	}{
		{name: "no window", config: map[string]any{"roots": []string{"/tmp"}}},
		{name: "until before since", config: map[string]any{"roots": []string{"/tmp"}}, since: until, until: since},
		{name: "missing roots", since: since, until: until},
		{name: "roots type", config: map[string]any{"roots": "proj"}, since: since, until: until},
		{name: "empty roots", config: map[string]any{"roots": []string{}}, since: since, until: until},
		{name: "blank root", config: map[string]any{"roots": []string{"  "}}, since: since, until: until},
		{name: "authors type", config: map[string]any{"roots": []string{"/tmp"}, "authors": "alice"}, since: since, until: until},
		{name: "user path", config: map[string]any{"roots": []string{"~alice/proj"}}, since: since, until: until},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Collect(context.Background(), source.CollectRequest{
				Since:  tc.since,
				Until:  tc.until,
				Config: tc.config,
			}, nil)
			if !errors.Is(err, source.ErrConfig) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestCollectCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New().Collect(ctx, source.CollectRequest{
		Since:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Config: map[string]any{"roots": []string{t.TempDir()}},
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestCollectWarnsForMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	result, err := New().Collect(context.Background(), source.CollectRequest{
		Since:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Until:  time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		Config: map[string]any{"roots": []string{missing}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Events != 0 || !logContains(result.Logs, "root not found: "+missing) {
		t.Fatalf("result = %#v", result)
	}
}

func TestScanDepth(t *testing.T) {
	root := t.TempDir()
	makeRepo := func(rel string) string {
		t.Helper()
		dir := root
		if rel != "" {
			dir = filepath.Join(root, rel)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	edge := makeRepo(filepath.Join("edge", "one", "two"))
	makeRepo(filepath.Join("skip", "one", "two", "three"))
	outer := makeRepo("outer")
	makeRepo(filepath.Join("outer", "vendor"))
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	repos, logs := scanRoots([]string{root, root})
	if len(logs) != 0 {
		t.Fatalf("logs = %#v", logs)
	}
	abs := func(path string) string {
		t.Helper()
		got, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		return filepath.Clean(got)
	}
	want := []string{abs(edge), abs(outer)}
	if !sameStrings(repos, want) {
		t.Fatalf("repos = %#v, want %#v", repos, want)
	}
}

func TestNormalizeRemote(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "git@github.com:JeremyJaymes/otto.git", want: "github.com/JeremyJaymes/otto"},
		{in: "https://github.com/JeremyJaymes/otto.git", want: "github.com/JeremyJaymes/otto"},
		{in: "https://github.com/JeremyJaymes/otto/", want: "github.com/JeremyJaymes/otto"},
		{in: "https://GitHub.com/Owner/Repo.git", want: "github.com/Owner/Repo"},
		{in: "https://user:s3cret-token@github.com/acme/widgets.git", want: "github.com/acme/widgets"},
		{in: "ssh://git@github.com/acme/widgets.git", want: "github.com/acme/widgets"},
		{in: "ssh://git@github.com:22/acme/widgets.git", want: "github.com/acme/widgets"},
		{in: "ssh://git@gitlab.com/group/sub/repo.git", want: "gitlab.com/group/sub/repo"},
		{in: "git@gitlab.com:group/sub/repo.git", want: "gitlab.com/group/sub/repo"},
		{in: "file:///tmp/repo.git", want: ""},
		{in: "/tmp/repo.git", want: ""},
		{in: "", want: ""},
	}
	for _, tc := range cases {
		if got := normalizeRemote(tc.in); got != tc.want {
			t.Fatalf("normalizeRemote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseRefs(t *testing.T) {
	message := "Handle retries #12\n\nFixes gen-301\nSee a1b2c3d\nEncode as UTF-8\nThis reverts commit abcdefabcdefabcdefabcdefabcdefabcdefabcd."
	got := parseRefs(message, "abcdefabcdefabcdefabcdefabcdefabcdefabcd")
	want := []string{"#12", "GEN-301", "a1b2c3d"}
	if !sameStrings(got, want) {
		t.Fatalf("refs = %#v", got)
	}
	self := "0123456789abcdef0123456789abcdef01234567"
	if refs := parseRefs("prefix "+self[:7]+" done", self); len(refs) != 0 {
		t.Fatalf("self short sha kept: %#v", refs)
	}
}

func TestCanonicalPath(t *testing.T) {
	cases := map[string]string{
		"app/retry.go":                           "app/retry.go",
		"app/{retry.go => retries.go}":           "app/retries.go",
		"app/{retries.go => file with space.go}": "app/file with space.go",
		"old/dir/file.go => new/dir/file.go":     "new/dir/file.go",
		`"app/quoted.go"`:                        "app/quoted.go",
	}
	for in, want := range cases {
		if got := canonicalPath(in); got != want {
			t.Fatalf("canonicalPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDescribe(t *testing.T) {
	manifest, err := New().Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "git" || manifest.Version != Version || manifest.Protocol != source.ProtocolVersion {
		t.Fatalf("manifest = %#v", manifest)
	}
	if !manifest.Config["roots"].Required || manifest.Config["roots"].Type != source.TypeStrings {
		t.Fatalf("roots = %#v", manifest.Config["roots"])
	}
	if manifest.Config["authors"].Required || manifest.Config["authors"].Type != source.TypeStrings {
		t.Fatalf("authors = %#v", manifest.Config["authors"])
	}
}

func assertCommit(t *testing.T, ev event.Event, cwd, repo, actor string, files, refs []string, added, deleted int) {
	t.Helper()
	if ev.CWD != cwd || ev.Repo != repo || ev.Actor != actor {
		t.Fatalf("commit %q cwd=%s repo=%s actor=%s", ev.Title, ev.CWD, ev.Repo, ev.Actor)
	}
	if files != nil && !sameStrings(ev.Files, files) {
		t.Fatalf("commit %q files = %#v, want %#v", ev.Title, ev.Files, files)
	}
	if refs != nil && !sameStrings(ev.Refs, refs) {
		t.Fatalf("commit %q refs = %#v, want %#v", ev.Title, ev.Refs, refs)
	}
	if ev.Metrics == nil || ev.Metrics["lines_added"] != float64(added) || ev.Metrics["lines_deleted"] != float64(deleted) {
		t.Fatalf("commit %q metrics = %#v", ev.Title, ev.Metrics)
	}
}

func logContains(logs []source.LogLine, msg string) bool {
	for _, line := range logs {
		if line.Level == "warn" && line.Msg == msg {
			return true
		}
	}
	return false
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func authorEnv(name, email, date string) []string {
	return []string{
		"GIT_AUTHOR_NAME=" + name,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + name,
		"GIT_COMMITTER_EMAIL=" + email,
		"GIT_COMMITTER_DATE=" + date,
	}
}

func commitAt(t *testing.T, dir, name, email, date, message string) {
	t.Helper()
	runGit(t, dir, authorEnv(name, email, date), "commit", "-m", message)
}

func writeRepoFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, extra []string, args ...string) {
	t.Helper()
	base := []string{
		"-c", "user.name=Alice",
		"-c", "user.email=alice@example.com",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
		"-c", "core.autocrlf=false",
	}
	cmd := exec.Command("git", append(base, args...)...)
	cmd.Dir = dir
	cmd.Env = mergeEnv(append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Alice",
		"GIT_AUTHOR_EMAIL=alice@example.com",
		"GIT_COMMITTER_NAME=Alice",
		"GIT_COMMITTER_EMAIL=alice@example.com",
	), extra)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	last := make(map[string]int, len(base)+len(extra))
	all := append(append([]string{}, base...), extra...)
	for i, item := range all {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			last[key] = i
		}
	}
	out := make([]string, 0, len(all))
	for i, item := range all {
		key, _, ok := strings.Cut(item, "=")
		if !ok || last[key] == i {
			out = append(out, item)
		}
	}
	return out
}
