package source

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// readLogicalLine reads through the next newline or EOF.
// The returned line does not include the newline. tooLong is set when the
// line is longer than max; the rest of that line is discarded.
func readLogicalLine(r *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		frag, readErr := r.ReadSlice('\n')
		chunk := append([]byte(nil), frag...)
		if !tooLong {
			if len(line)+len(chunk) > max {
				tooLong = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if tooLong {
			return nil, true, readErr
		}
		return trimNL(line), false, readErr
	}
}

func trimNL(line []byte) []byte {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line
}

// parseCollect reads JSON lines. Invalid lines increment the dropped count
// and do not stop the scan. Events are appended and not emitted yet.
func parseCollect(name string, r io.Reader, result *Result, events *[]event.Event) (int, error) {
	br := bufio.NewReader(r)
	dropped := 0
	for {
		line, tooLong, err := readLogicalLine(br, maxMessageBytes)
		if tooLong {
			dropped++
		} else if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if !applyLine(name, trimmed, result, events) {
				dropped++
			}
		}
		if err == io.EOF {
			return dropped, nil
		}
		if err != nil {
			return dropped, err
		}
	}
}

func applyLine(name string, line []byte, result *Result, events *[]event.Event) bool {
	var msg wireLine
	if err := json.Unmarshal(line, &msg); err != nil {
		return false
	}
	switch msg.Type {
	case lineEvent:
		ev, err := acceptEvent(name, msg.Event)
		if err != nil {
			return false
		}
		*events = append(*events, ev)
		return true
	case lineLog:
		logLine, err := acceptLog(msg.Level, msg.Msg)
		if err != nil {
			return false
		}
		result.Logs = append(result.Logs, logLine)
		return true
	case lineCursor:
		if msg.Cursor == nil || len(*msg.Cursor) > maxCursorBytes || !printable(*msg.Cursor) {
			return false
		}
		result.Cursor = *msg.Cursor
		result.HasCursor = true
		return true
	default:
		return false
	}
}
