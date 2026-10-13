package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

func TestMain(m *testing.M) {
	if os.Getenv("WEEKLOG_SOURCE_TEST_HELPER") == "1" {
		os.Exit(runSourceTestHelper())
	}
	os.Exit(m.Run())
}

func runSourceTestHelper() int {
	mode := os.Getenv("WEEKLOG_HELPER_MODE")
	if mode == "" {
		mode = "ok"
	}
	secret := os.Getenv("WEEKLOG_SECRET_MARK")
	argc := len(os.Args)
	arg1 := ""
	if argc > 1 {
		arg1 = os.Args[1]
	}
	saw := "no"
	if secret != "" {
		for _, arg := range os.Args {
			if strings.Contains(arg, secret) {
				saw = "args"
			}
		}
	}

	writeHelperStatus(argc, arg1, saw, "null", "0")

	switch mode {
	case "sleep":
		time.Sleep(30 * time.Second)
		return ExitOK
	case "exit1":
		helperPrintf(os.Stderr, "boom\n")
		return ExitFailed
	case "exit2":
		helperPrintf(os.Stderr, "missing token\n")
		return ExitConfig
	case "badjson":
		helperPrintf(os.Stdout, "not-json\n")
		return ExitOK
	case "protocol":
		helperPrintf(os.Stdout, "{\"protocol\":99,\"name\":\"todoist\",\"version\":\"0.1.0\",\"config\":{}}\n")
		return ExitOK
	case "name":
		helperPrintf(os.Stdout, "{\"protocol\":1,\"name\":\"other\",\"version\":\"0.1.0\",\"config\":{}}\n")
		return ExitOK
	case "serve-fixture":
		return Serve(fixtureSource{name: helperPluginName()}, os.Args[1:])
	}

	name := helperPluginName()
	if arg1 == "describe" {
		helperPrintf(os.Stdout, "{\"protocol\":1,\"name\":%q,\"version\":\"0.1.0\",\"config\":{\"token\":{\"type\":\"secret\",\"required\":true},\"account_id\":{\"type\":\"string\",\"required\":true}}}\n", name)
		writeHelperStatus(argc, arg1, saw, "null", "1")
		return ExitOK
	}
	if arg1 != "collect" || argc != 2 {
		helperPrintf(os.Stderr, "usage: describe | collect\n")
		return ExitFailed
	}

	data, err := readHelperStdin()
	if err != nil {
		helperPrintf(os.Stderr, "cannot read request\n")
		return ExitConfig
	}
	if secret != "" && bytes.Contains(data, []byte(secret)) && saw != "args" {
		saw = "yes"
	}
	req, err := DecodeRequest(data)
	protocol := "invalid"
	cursor := "null"
	if err == nil {
		protocol = fmt.Sprintf("%d", req.Protocol)
		if req.Cursor != nil {
			cursor = *req.Cursor
		}
	}
	writeHelperStatus(argc, arg1, saw, cursor, protocol)

	switch mode {
	case "badlines":
		helperPrintf(os.Stdout, "{\"type\":\"event\",\"event\":{\"source\":%q,\"source_id\":\"e1\",\"kind\":\"commit\",\"ts\":\"2026-10-08T00:00:00Z\",\"title\":\"one\"}}\n", name)
		helperPrintf(os.Stdout, "not json\n")
		helperPrintf(os.Stdout, "{\"type\":\"event\",\"event\":{\"source\":\"nope\",\"source_id\":\"e2\",\"kind\":\"commit\",\"ts\":\"2026-10-08T00:00:00Z\"}}\n")
		helperPrintf(os.Stdout, "{\"type\":\"nope\"}\n")
		helperPrintf(os.Stdout, "{\"type\":\"event\",\"event\":{\"source_id\":\"e3\",\"kind\":\"commit\",\"ts\":\"2026-10-08T01:00:00Z\",\"title\":\"three\"}}\n")
		helperPrintf(os.Stdout, "{\"type\":\"log\",\"level\":\"warn\",\"msg\":\"skipped 2 archived projects\"}\n")
		helperPrintf(os.Stdout, "{\"type\":\"cursor\",\"cursor\":\"cur-1\"}\n")
		return ExitOK
	case "hugeline":
		helperPrintf(os.Stdout, "%s\n", strings.Repeat("x", 1000))
		helperPrintf(os.Stdout, "{\"type\":\"event\",\"event\":{\"source_id\":\"e1\",\"kind\":\"commit\",\"ts\":\"2026-10-08T00:00:00Z\"}}\n")
		return ExitOK
	default:
		helperPrintf(os.Stdout, "{\"type\":\"event\",\"event\":{\"source\":%q,\"source_id\":\"e1\",\"kind\":\"ticket\",\"ts\":\"2026-10-08T14:02:11Z\",\"title\":\"Ship helper\"}}\n", name)
		helperPrintf(os.Stdout, "{\"type\":\"log\",\"level\":\"warning\",\"msg\":\"skipped 2 archived projects\"}\n")
		helperPrintf(os.Stdout, "{\"type\":\"cursor\",\"cursor\":\"cur-1\"}\n")
		return ExitOK
	}
}

func helperPrintf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func helperPluginName() string {
	if len(os.Args) == 0 {
		return "helper"
	}
	name, ok := pluginName(filepath.Base(os.Args[0]))
	if !ok {
		return "helper"
	}
	return name
}

func readHelperStdin() ([]byte, error) {
	return io.ReadAll(io.LimitReader(os.Stdin, int64(maxMessageBytes)+1))
}

func writeHelperStatus(argc int, arg1, saw, cursor, protocol string) {
	path := os.Getenv("WEEKLOG_HELPER_STATUS")
	if path == "" {
		return
	}
	text := fmt.Sprintf("argc=%d\narg1=%s\nsaw_secret=%s\ncursor=%s\nprotocol=%s\n", argc, arg1, saw, cursor, protocol)
	if secret := os.Getenv("WEEKLOG_SECRET_MARK"); secret != "" && strings.Contains(text, secret) {
		text = "status leaked\n"
	}
	_ = os.WriteFile(path, []byte(text), 0o600)
}

// fixtureSource is a built-in-shaped source used to prove Serve and Process
// agree on the protocol.
type fixtureSource struct {
	name string
}

func (f fixtureSource) Describe(ctx context.Context) (Manifest, error) {
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Protocol: ProtocolVersion,
		Name:     f.name,
		Version:  "0.1.0",
		Config: map[string]Field{
			"token": {Type: TypeSecret, Required: true},
		},
	}, nil
}

func (f fixtureSource) Collect(ctx context.Context, req CollectRequest, emit func(event.Event) error) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := emit(event.Event{
		Source:   f.name,
		SourceID: "e1",
		Kind:     event.KindTicket,
		TS:       time.Date(2026, 10, 8, 14, 2, 11, 0, time.UTC),
		Title:    "Ship helper",
	}); err != nil {
		return Result{}, err
	}
	cursor := "cur-1"
	if req.Cursor != nil {
		cursor = "next-" + *req.Cursor
	}
	return Result{
		HasCursor: true,
		Cursor:    cursor,
		Logs:      []LogLine{{Level: "warn", Msg: "skipped 2 archived projects"}},
	}, nil
}
