package linear

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Version is the describe version of the linear collector.
const Version = "0.1.0"

const sourceName = "linear"

// Source collects Linear issues and state history.
type Source struct {
	endpoint string
	client   *http.Client
}

// New returns the built-in linear source.
func New() *Source {
	return &Source{}
}

// Describe returns the linear config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     sourceName,
		Version:  Version,
		Config: map[string]source.Field{
			"token":          {Type: source.TypeSecret, Required: true},
			"ticket_pattern": {Type: source.TypeString},
		},
	}, nil
}

// Collect emits issues assigned to or created by the authenticated user that
// were updated in the window, and state changes from those issues' history
// whose time falls in the window. The window includes since and excludes
// until. token is required. ticket_pattern selects keys stored in refs.
func (s *Source) Collect(ctx context.Context, req source.CollectRequest, emit func(event.Event) error) (source.Result, error) {
	if err := ctx.Err(); err != nil {
		return source.Result{}, err
	}
	s = s.withLoopback(req.Config)
	since, until, err := collectionWindow(req)
	if err != nil {
		return source.Result{}, err
	}
	token, err := resolveToken(req.Config)
	if err != nil {
		return source.Result{}, err
	}
	match, err := compilePattern(req.Config)
	if err != nil {
		return source.Result{}, err
	}

	issues, logs, err := s.issues(ctx, token, since, until)
	if err != nil {
		return source.Result{Logs: logs}, err
	}
	events, hist, err := buildEvents(issues, match, since, until)
	logs = append(logs, hist.logs()...)
	if err != nil {
		return source.Result{Logs: logs}, err
	}
	sortEvents(events)

	result := source.Result{Logs: logs}
	for _, ev := range events {
		if emit != nil {
			if err := emit(ev); err != nil {
				return result, err
			}
		}
		result.Events++
	}
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

func resolveToken(config map[string]any) (string, error) {
	if config == nil {
		return "", configErr("token is unset")
	}
	value, ok := config["token"]
	if !ok || value == nil {
		return "", configErr("token is unset")
	}
	text, ok := value.(string)
	if !ok {
		return "", configErr("token must be a string")
	}
	text = cleanToken(text)
	if text == "" {
		return "", configErr("token is unset")
	}
	if strings.IndexFunc(text, func(r rune) bool {
		return r <= ' ' || r == 0x7f
	}) >= 0 {
		return "", configErr("token contains whitespace or control characters")
	}
	return text, nil
}

func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func (s *Source) issues(ctx context.Context, token string, since, until time.Time) ([]issueItem, []source.LogLine, error) {
	nodes, err := s.search(ctx, token, since, until)
	if err != nil {
		return nil, nil, err
	}
	var (
		kept    []issueItem
		skipped skipStats
		seen    = map[string]struct{}{}
	)
	for _, node := range nodes {
		item, stat := decodeIssue(node)
		if stat != decodedOK {
			skipped.add(stat)
			continue
		}
		if _, ok := seen[item.Identifier]; ok {
			continue
		}
		_, stat = item.activity(since, until)
		if stat != decodedOK {
			skipped.add(stat)
			continue
		}
		if err := s.pageHistory(ctx, token, &item); err != nil {
			return nil, skipped.logs(), err
		}
		seen[item.Identifier] = struct{}{}
		kept = append(kept, item)
	}
	return kept, skipped.logs(), nil
}

func sortEvents(events []event.Event) {
	sort.Slice(events, func(i, j int) bool {
		if !events[i].TS.Equal(events[j].TS) {
			return events[i].TS.Before(events[j].TS)
		}
		if events[i].Kind != events[j].Kind {
			return events[i].Kind < events[j].Kind
		}
		return events[i].SourceID < events[j].SourceID
	})
}

func configErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrConfig}
}

func failedErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrFailed}
}

type skipKind int

const (
	decodedSkip skipKind = iota
	decodedOK
	decodedMalformed
	decodedUnexpected
)

type skipStats struct {
	malformed  int
	unexpected int
	history    int
}

func (s *skipStats) add(kind skipKind) {
	switch kind {
	case decodedMalformed:
		s.malformed++
	case decodedUnexpected:
		s.unexpected++
	}
}

func (s skipStats) logs() []source.LogLine {
	var logs []source.LogLine
	if s.malformed > 0 {
		logs = append(logs, warn(counted(s.malformed, "malformed issue", "malformed issues")))
	}
	if s.unexpected > 0 {
		logs = append(logs, warn(counted(s.unexpected, "unexpected Linear result", "unexpected Linear results")))
	}
	if s.history > 0 {
		logs = append(logs, warn(counted(s.history, "malformed history entry", "malformed history entries")))
	}
	return logs
}

func warn(msg string) source.LogLine {
	return source.LogLine{Level: "warn", Msg: msg}
}

func counted(n int, singular, plural string) string {
	if n == 1 {
		return "skipped 1 " + singular
	}
	return fmt.Sprintf("skipped %d %s", n, plural)
}
