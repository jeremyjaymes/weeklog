package linear

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/linker"
	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/sources/git"
	"github.com/jeremyjaymes/weeklog/internal/store"
)

func TestLinearTicketLinksGitCommit(t *testing.T) {
	const pattern = `GEN-\d+`
	srv, _ := newFixtureServer(t)
	linearEvents, _ := collectFrom(t, srv.URL, request("fixture-token", pattern))
	ticket := findEvent(t, linearEvents, "GEN-301")
	if ticket.Kind != event.KindTicket || !hasRef(ticket.Refs, "GEN-301") {
		t.Fatalf("ticket refs = %#v", ticket.Refs)
	}

	repo := t.TempDir()
	runGit(t, repo, nil, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "note.txt"), []byte("note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, nil, "add", "note.txt")
	runGit(t, repo, authorEnv("Alice", "alice@example.com", "2026-10-08T15:00:00Z"), "commit", "-m", "Fix GEN-301 retries")

	var commits []event.Event
	_, err := git.New().Collect(context.Background(), source.CollectRequest{
		Since: windowSince,
		Until: windowUntil,
		Config: map[string]any{
			"roots":   []string{repo},
			"authors": []string{"alice@example.com"},
		},
	}, func(ev event.Event) error {
		commits = append(commits, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 || commits[0].Kind != event.KindCommit || !hasRef(commits[0].Refs, "GEN-301") {
		t.Fatalf("commits = %#v", commits)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "weeklog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.UpsertEvents(ctx, append(linearEvents, commits...)); err != nil {
		t.Fatal(err)
	}
	if err := linker.RunWith(ctx, st, windowSince, windowUntil, linker.Options{
		TicketPatterns: []string{pattern},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListWorkItems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var matched bool
	for _, item := range items {
		var hasTicket, hasCommit bool
		for _, member := range item.Events {
			if member.Source == "linear" && member.SourceID == "GEN-301" && member.Kind == event.KindTicket {
				hasTicket = strings.Contains(member.Reason, "ref:GEN-301")
			}
			if member.Source == "git" && member.SourceID == commits[0].SourceID && member.Kind == event.KindCommit {
				hasCommit = strings.Contains(member.Reason, "ref:GEN-301")
			}
		}
		if hasTicket && hasCommit {
			matched = true
			if item.Title != "Fix Stripe webhook retries" {
				t.Fatalf("title = %q", item.Title)
			}
		}
	}
	if !matched {
		t.Fatalf("GEN-301 was not linked to %s\n%#v", commits[0].SourceID, items)
	}
}

func hasRef(refs []string, key string) bool {
	for _, ref := range refs {
		if ref == key {
			return true
		}
	}
	return false
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
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
	)
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
