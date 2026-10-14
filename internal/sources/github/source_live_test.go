//go:build live

package github

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

// TestLiveSmoke calls the GitHub API when a token is available.
// go test -tags live ./internal/sources/github
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" {
		if _, err := exec.LookPath("gh"); err != nil {
			t.Skip("no GITHUB_TOKEN and gh is not installed")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req := source.CollectRequest{
		Since: time.Now().Add(-24 * time.Hour),
		Until: time.Now(),
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Config = map[string]any{"token": token}
	}
	if login := os.Getenv("GITHUB_LOGIN"); login != "" {
		req.Identity.Handles = map[string]string{"github": login}
	}
	_, err := New().Collect(ctx, req, func(event.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}
