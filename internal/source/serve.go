package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// Serve runs src as a protocol v1 plugin.
// args is the argument list after the executable name and must be
// "describe" or "collect". Serve reads the collect request from stdin and
// writes protocol messages to stdout. Diagnostics go to stderr and do not
// include the request body.
// The return value is ExitOK, ExitFailed, or ExitConfig.
func Serve(src Source, args []string) int {
	if src == nil {
		writeStderr("source is missing\n")
		return ExitFailed
	}
	if len(args) != 1 {
		writeStderr("usage: describe | collect\n")
		return ExitFailed
	}
	switch args[0] {
	case "describe":
		return serveDescribe(src)
	case "collect":
		return serveCollect(src)
	default:
		writeStderr("usage: describe | collect\n")
		return ExitFailed
	}
}

func serveDescribe(src Source) int {
	manifest, err := src.Describe(context.Background())
	if err != nil {
		writeStderr("%s\n", oneLine(err.Error()))
		return exitCode(err)
	}
	buf, err := EncodeManifest(manifest)
	if err != nil {
		writeStderr("%s\n", oneLine(err.Error()))
		return exitCode(err)
	}
	if _, err := os.Stdout.Write(buf); err != nil {
		return ExitFailed
	}
	return ExitOK
}

func serveCollect(src Source) int {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, int64(maxMessageBytes)+1))
	if err != nil {
		writeStderr("cannot read collect request\n")
		return ExitConfig
	}
	if len(data) > maxMessageBytes {
		writeStderr("collect request exceeds %d bytes\n", maxMessageBytes)
		return ExitConfig
	}
	req, err := DecodeRequest(data)
	if err != nil {
		writeStderr("%s\n", oneLine(err.Error()))
		return ExitConfig
	}
	result, err := src.Collect(context.Background(), req, writeEvent)
	if err != nil {
		writeStderr("%s\n", oneLine(err.Error()))
		return exitCode(err)
	}
	for _, line := range result.Logs {
		if err := writeLog(line); err != nil {
			return ExitFailed
		}
	}
	if result.HasCursor {
		if err := writeCursor(result.Cursor); err != nil {
			return ExitFailed
		}
	}
	return ExitOK
}

func writeEvent(ev event.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	buf, err := encodeLine(wireLine{Type: lineEvent, Event: payload})
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(buf)
	return err
}

func writeLog(line LogLine) error {
	buf, err := encodeLine(wireLine{Type: lineLog, Level: line.Level, Msg: line.Msg})
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(buf)
	return err
}

func writeCursor(cursor string) error {
	buf, err := encodeLine(wireLine{Type: lineCursor, Cursor: &cursor})
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(buf)
	return err
}

func exitCode(err error) int {
	if err == nil || errors.Is(err, ErrConfig) {
		if err == nil {
			return ExitOK
		}
		return ExitConfig
	}
	return ExitFailed
}

func writeStderr(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format, args...)
}
