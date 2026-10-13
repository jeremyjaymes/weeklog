package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestDocumentedPythonExample(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	script := pythonExample(t)
	dir := t.TempDir()
	path := filepath.Join(dir, PluginPrefix+"example")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	proc := &Process{Name: "example", Path: path, Timeout: 5 * time.Second}
	manifest, err := proc.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "example" || manifest.Version != "0.1.0" || !manifest.Config["token"].Required {
		t.Fatalf("manifest = %#v", manifest)
	}
	const secret = "wksec_EXAMPLE_not_in_errors"
	var got []event.Event
	result, err := proc.Collect(context.Background(), sampleRequest(secret), func(ev event.Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Source != "example" || got[0].SourceID != "example-1" || got[0].Kind != event.KindComment {
		t.Fatalf("events = %#v", got)
	}
	if result.Cursor != "example-cursor" || !result.HasCursor {
		t.Fatalf("result = %#v", result)
	}
}

func pythonExample(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "PLUGIN_PROTOCOL.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const start = "```python\n"
	const end = "```\n"
	text := string(body)
	i := strings.Index(text, start)
	if i < 0 {
		t.Fatal("protocol doc has no python example")
	}
	text = text[i+len(start):]
	j := strings.Index(text, end)
	if j < 0 {
		t.Fatal("protocol doc python example is not closed")
	}
	return text[:j]
}
