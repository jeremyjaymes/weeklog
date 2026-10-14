package codex

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

const cursorVersion = 1

// cursorDoc is the opaque collect cursor. Paths are relative to the Codex
// home. A file is left unread when its size and mtime still match and its
// recorded session start is outside the requested window.
type cursorDoc struct {
	V     int                  `json:"v"`
	Files map[string]cursorRec `json:"files"`
}

type cursorRec struct {
	M int64  `json:"m"`
	S int64  `json:"s"`
	T string `json:"t,omitempty"`
}

func decodeCursor(raw *string) (cursorDoc, []source.LogLine) {
	empty := cursorDoc{V: cursorVersion, Files: map[string]cursorRec{}}
	if raw == nil {
		return empty, nil
	}
	text := strings.TrimSpace(*raw)
	if text == "" {
		return empty, nil
	}
	var doc cursorDoc
	if err := json.Unmarshal([]byte(text), &doc); err != nil || doc.V != cursorVersion {
		return empty, []source.LogLine{warn("ignored unreadable cursor")}
	}
	if doc.Files == nil {
		doc.Files = map[string]cursorRec{}
	}
	return doc, nil
}

func encodeCursor(doc cursorDoc) (string, error) {
	if doc.Files == nil {
		doc.Files = map[string]cursorRec{}
	}
	doc.V = cursorVersion
	buf, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func putCursor(doc cursorDoc, files []scannedFile, start string) {
	if doc.Files == nil {
		return
	}
	for _, file := range files {
		doc.Files[file.rel] = cursorRec{M: file.mtime, S: file.size, T: start}
	}
}

// reusable reports whether every file still matches the cursor and the
// session does not need to be read for this window.
func reusable(files []scannedFile, cur cursorDoc, since, until time.Time) (string, bool) {
	if len(files) == 0 || cur.Files == nil {
		return "", false
	}
	var (
		start      string
		seenRecord bool
	)
	for _, file := range files {
		rec, ok := cur.Files[file.rel]
		if !ok || rec.M != file.mtime || rec.S != file.size {
			return "", false
		}
		if !seenRecord {
			start = rec.T
			seenRecord = true
			continue
		}
		if rec.T != start {
			return "", false
		}
	}
	if start == "" {
		return "", true
	}
	t, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return "", false
	}
	t = t.UTC()
	if !t.Before(since) && t.Before(until) {
		return "", false
	}
	return start, true
}
