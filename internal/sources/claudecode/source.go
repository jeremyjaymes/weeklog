package claudecode

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Version is the describe version of the claude-code collector.
const Version = "0.1.0"

const (
	sourceName = "claude-code"

	// maxTitleRunes bounds the event title. Summary is bounded by event.Prepare.
	maxTitleRunes = 200

	// maxCursorBytes matches the protocol limit for one cursor string.
	maxCursorBytes = 64 << 10
)

// Source collects Claude Code session transcripts from the local config dir.
type Source struct{}

// New returns the built-in claude-code source.
func New() *Source {
	return &Source{}
}

// Describe returns the claude-code config schema.
// The collector reads $CLAUDE_CONFIG_DIR or ~/.claude and takes no config keys.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     sourceName,
		Version:  Version,
		Config:   map[string]source.Field{},
	}, nil
}

// Collect emits one agent_session for each Claude Code session whose start
// falls in [since, until). Sub-agent transcripts fold into the parent session.
// The cursor records each transcript's size and modification time. A file
// whose fingerprint still matches is read again only when its session start
// lies in the requested window, so a later week can still be collected.
func (s *Source) Collect(ctx context.Context, req source.CollectRequest, emit func(event.Event) error) (source.Result, error) {
	if err := ctx.Err(); err != nil {
		return source.Result{}, err
	}
	since, until, err := collectionWindow(req)
	if err != nil {
		return source.Result{}, err
	}
	dir, err := configDir()
	if err != nil {
		return source.Result{}, configErr(err.Error())
	}

	cur, logs := decodeCursor(req.Cursor)
	groups, scanLogs, err := scanProjects(dir)
	if err != nil {
		return source.Result{}, err
	}
	logs = append(logs, scanLogs...)

	next := cursorDoc{V: cursorVersion, Files: map[string]cursorRec{}}
	var (
		events   []event.Event
		dropped  int
		timeless int
	)
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return source.Result{}, err
		}
		if stamp, ok := reusable(group.files, cur, since, until); ok {
			putCursor(next, group.files, stamp)
			continue
		}
		parsed, body, n, incomplete, err := parseGroup(group)
		if err != nil {
			logs = append(logs, warn("cannot read session "+group.id+": "+oneLine(err.Error())))
			continue
		}
		dropped += n
		stamp := ""
		if parsed.hasTime {
			stamp = parsed.start.UTC().Format(time.RFC3339Nano)
		} else {
			timeless++
		}
		if !incomplete {
			putCursor(next, group.files, stamp)
		} else {
			logs = append(logs, warn("session line exceeds 4 MiB: "+group.id))
		}
		ev, ok, err := sessionEvent(parsed, group.id, body, since, until)
		if err != nil {
			return source.Result{}, failedErr("session " + group.id + ": " + oneLine(err.Error()))
		}
		if !ok {
			continue
		}
		events = append(events, ev)
	}
	sortEvents(events)

	result := source.Result{Logs: logs, Dropped: dropped}
	if timeless > 0 {
		result.Logs = append(result.Logs, warn(counted(timeless, "session with no timestamp", "sessions with no timestamp")))
	}
	if dropped > 0 {
		result.Logs = append(result.Logs, warn(counted(dropped, "unreadable session line", "unreadable session lines")))
	}
	for _, ev := range events {
		if emit != nil {
			if err := emit(ev); err != nil {
				return result, err
			}
		}
		result.Events++
	}

	encoded, err := encodeCursor(next)
	if err != nil {
		return result, failedErr("cannot encode cursor")
	}
	if len(encoded) > maxCursorBytes {
		result.Logs = append(result.Logs, warn("cursor exceeds 64 KiB; keeping the previous cursor"))
		return result, nil
	}
	result.Cursor = encoded
	result.HasCursor = true
	return result, nil
}

func collectionWindow(req source.CollectRequest) (time.Time, time.Time, error) {
	if req.Since.IsZero() || req.Until.IsZero() {
		return time.Time{}, time.Time{}, configErr("since and until are required")
	}
	if !req.Until.After(req.Since) {
		return time.Time{}, time.Time{}, configErr("until must be after since")
	}
	return req.Since, req.Until, nil
}

func configDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		return expandPath(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

func sessionEvent(p partial, id, body string, since, until time.Time) (event.Event, bool, error) {
	if id == "" || !p.hasTime {
		return event.Event{}, false, nil
	}
	start := p.start.UTC()
	if start.Before(since) || !start.Before(until) {
		return event.Event{}, false, nil
	}
	prompt := firstPrompt(p)
	ev := event.Event{
		Source:   sourceName,
		SourceID: id,
		Kind:     event.KindAgentSession,
		TS:       start,
		Title:    truncateRunes(firstLine(prompt), maxTitleRunes),
		Summary:  prompt,
		Branch:   p.branch,
		CWD:      p.cwd,
		Files:    displayFiles(p.cwd, p.files),
		BodyRef:  fileURL(body),
	}
	if p.end.After(start) {
		end := p.end.UTC()
		ev.TSEnd = &end
	}
	if p.tokensIn > 0 || p.tokensOut > 0 {
		ev.Metrics = map[string]float64{}
		if p.tokensIn > 0 {
			ev.Metrics["tokens_in"] = p.tokensIn
		}
		if p.tokensOut > 0 {
			ev.Metrics["tokens_out"] = p.tokensOut
		}
	}
	prepared, err := event.Prepare(ev)
	if err != nil {
		return event.Event{}, false, err
	}
	return prepared, true, nil
}

func firstPrompt(p partial) string {
	switch {
	case strings.TrimSpace(p.prompt) != "":
		return strings.TrimSpace(p.prompt)
	case strings.TrimSpace(p.sidePrompt) != "":
		return strings.TrimSpace(p.sidePrompt)
	default:
		return strings.TrimSpace(p.metaPrompt)
	}
}

func displayFiles(cwd string, files []string) []string {
	if len(files) == 0 {
		return nil
	}
	out := make([]string, 0, len(files))
	for _, file := range files {
		if rel := displayPath(cwd, file); rel != "" {
			out = append(out, rel)
		}
	}
	return out
}

func displayPath(cwd, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if cwd == "" || !filepath.IsAbs(cleaned) {
		return cleaned
	}
	rel, err := filepath.Rel(filepath.Clean(cwd), cleaned)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return cleaned
	}
	return rel
}

func fileURL(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(filepath.Clean(abs))
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

func expandPath(path string) (string, error) {
	expanded, err := expandUser(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if expanded == "" {
		return "", fmt.Errorf("claude config dir is empty")
	}
	return filepath.Clean(expanded), nil
}

func expandUser(path string) (string, error) {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand ~: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:]), nil
	}
	return "", fmt.Errorf("expand %q: ~user paths are not supported", path)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for r := 0; r < n; r++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

func sortEvents(events []event.Event) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].TS.Equal(events[j].TS) {
			return events[i].TS.Before(events[j].TS)
		}
		return events[i].SourceID < events[j].SourceID
	})
}

func counted(n int, singular, plural string) string {
	if n == 1 {
		return "skipped 1 " + singular
	}
	return fmt.Sprintf("skipped %d %s", n, plural)
}

func oneLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func warn(msg string) source.LogLine {
	return source.LogLine{Level: "warn", Msg: msg}
}

func configErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrConfig}
}

func failedErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrFailed}
}
