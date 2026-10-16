//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWeekImageWritesStyledRecap(t *testing.T) {
	home := isolatePlugins(t)
	writeFreezeStub(t, filepath.Join(home, "bin"))
	cfgPath := writeWeekConfig(t, home, "")
	dbPath := filepath.Join(home, "weeklog.db")
	seedGraphWeekDB(t, dbPath)

	png := filepath.Join(home, "share", "recap.png")
	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--image", png)
	if err != nil {
		t.Fatalf("week: %v\n%s\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "# Week 2026-W41") || !strings.HasSuffix(stdout, "wrote "+png+"\n") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(string(mustRead(t, png)), "png") {
		t.Fatalf("png = %q", mustRead(t, png))
	}
	assertFreezeCapture(t, png, true)

	t.Setenv("NO_COLOR", "1")
	svg := filepath.Join(home, "recap.SVG")
	stdout, stderr, err = runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--no-graph", "--plain", "--image", svg)
	if err != nil {
		t.Fatalf("svg: %v\n%s\n%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "\x1b") || !strings.Contains(stdout, "# Week 2026-W41") || !strings.HasSuffix(stdout, "wrote "+svg+"\n") {
		t.Fatalf("stdout = %s", stdout)
	}
	if !strings.Contains(string(mustRead(t, svg)), "<svg") {
		t.Fatalf("svg = %q", mustRead(t, svg))
	}
	stdin := assertFreezeCapture(t, svg, false)
	if strings.Contains(stdin, "Session chain") {
		t.Fatalf("--no-graph image still drew the chain\n%s", stdin)
	}
}

func TestWeekImageFreezeFailure(t *testing.T) {
	home := isolatePlugins(t)
	writeFreezeStub(t, filepath.Join(home, "bin"))
	t.Setenv("WEEKLOG_TEST_FREEZE_FAIL", "1")
	cfgPath := writeWeekConfig(t, home, filepath.Join(home, "journal"))
	dbPath := filepath.Join(home, "weeklog.db")
	seedWeekDB(t, dbPath)
	image := filepath.Join(home, "recap.svg")

	stdout, stderr, err := runWeeklog(t, "--config", cfgPath, "week", "--week", "2026-W41", "--db", dbPath, "--image", image)
	if err == nil || !strings.Contains(err.Error(), "freeze blew up") {
		t.Fatalf("err = %v\n%s", err, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, statErr := os.Stat(image); !os.IsNotExist(statErr) {
		t.Fatalf("image exists after freeze failed: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, "journal", "2026-W41.md")); !os.IsNotExist(statErr) {
		t.Fatalf("wrote the report after freeze failed: %v", statErr)
	}
}

func assertFreezeCapture(t *testing.T, image string, wantChain bool) string {
	t.Helper()
	args := string(mustRead(t, image+".args"))
	for _, want := range []string{"--config\nbase\n", "--language\nansi\n", "--output\n" + image + "\n"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args missing %q\n%s", want, args)
		}
	}
	stdin := string(mustRead(t, image+".stdin"))
	if !strings.Contains(stdin, "\x1b") || !strings.Contains(stdin, "Week 2026-W41") || strings.Contains(stdin, "# Week") {
		t.Fatalf("freeze stdin is not the styled recap\n%s", stdin)
	}
	if wantChain && !strings.Contains(stdin, "Session chain") {
		t.Fatalf("freeze stdin missing the chain\n%s", stdin)
	}
	return stdin
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func writeFreezeStub(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `#!/bin/sh
set -eu
out=""
: > "$HOME/freeze.args.tmp"
while [ "$#" -gt 0 ]; do
  printf '%s\n' "$1" >> "$HOME/freeze.args.tmp"
  case "$1" in
    --output)
      out="$2"
      printf '%s\n' "$2" >> "$HOME/freeze.args.tmp"
      shift 2
      ;;
    --language|--config)
      printf '%s\n' "$2" >> "$HOME/freeze.args.tmp"
      shift 2
      ;;
    *)
      printf '%s\n' "unexpected arg: $1" >&2
      exit 2
      ;;
  esac
done
if [ -z "$out" ]; then
  printf '%s\n' "missing --output" >&2
  exit 2
fi
if [ "${WEEKLOG_TEST_FREEZE_FAIL:-}" != "" ]; then
  printf '%s\n' "freeze blew up" >&2
  exit 1
fi
/bin/cat > "$out.stdin"
/bin/mv "$HOME/freeze.args.tmp" "$out.args"
case "$out" in
  *.svg|*.SVG)
    printf '%s\n' '<svg xmlns="http://www.w3.org/2000/svg"></svg>' > "$out"
    ;;
  *.png|*.PNG)
    printf '%s\n' png > "$out"
    ;;
  *)
    printf '%s\n' "unsupported output $out" >&2
    exit 2
    ;;
esac
`
	path := filepath.Join(dir, "freeze")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
