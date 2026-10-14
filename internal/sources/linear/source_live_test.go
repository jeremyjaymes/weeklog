//go:build live

package linear

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

// TestLiveSmoke calls the Linear API when LINEAR_API_KEY is set.
// go test -tags live ./internal/sources/linear
func TestLiveSmoke(t *testing.T) {
	token := os.Getenv("LINEAR_API_KEY")
	if token == "" {
		t.Skip("LINEAR_API_KEY is unset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req := source.CollectRequest{
		Since:  time.Now().Add(-24 * time.Hour),
		Until:  time.Now(),
		Config: map[string]any{"token": token},
	}
	if pattern := os.Getenv("LINEAR_TICKET_PATTERN"); pattern != "" {
		req.Config["ticket_pattern"] = pattern
	}
	_, err := New().Collect(ctx, req, func(event.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}
