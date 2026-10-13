package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/event"
	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Version is the describe version of the git collector.
const Version = "0.1.0"

const sourceName = "git"

// maxScanDepth is how many directory levels below a root are scanned.
// The root itself is level 0. A repository stops the walk.
const maxScanDepth = 3

// Source collects commit events from local git repositories.
type Source struct{}

// New returns the built-in git source.
func New() *Source {
	return &Source{}
}

// Describe returns the git config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     sourceName,
		Version:  Version,
		Config: map[string]source.Field{
			"roots":   {Type: source.TypeStrings, Required: true},
			"authors": {Type: source.TypeStrings},
		},
	}, nil
}

// Collect emits commit events for repositories under roots.
// The window applies to the author date and includes since, excluding until.
// authors filters by email or name. When authors is omitted, identity emails
// are used. An empty authors list collects every author.
func (s *Source) Collect(ctx context.Context, req source.CollectRequest, emit func(event.Event) error) (source.Result, error) {
	if err := ctx.Err(); err != nil {
		return source.Result{}, err
	}
	opts, err := parseOptions(req)
	if err != nil {
		return source.Result{}, err
	}
	repos, logs := scanRoots(opts.roots)
	if len(repos) > 0 {
		if _, err := lookGit(); err != nil {
			return source.Result{Logs: logs}, err
		}
	}

	var events []event.Event
	seen := make(map[string]int)
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return source.Result{}, err
		}
		found, err := collectRepo(ctx, repo, opts)
		if err != nil {
			return source.Result{}, err
		}
		for _, ev := range found {
			if i, ok := seen[ev.SourceID]; ok {
				if events[i].Repo == "" && ev.Repo != "" {
					events[i] = ev
				}
				continue
			}
			seen[ev.SourceID] = len(events)
			events = append(events, ev)
		}
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

type options struct {
	roots   []string
	since   time.Time
	until   time.Time
	authors authorFilter
}

func parseOptions(req source.CollectRequest) (options, error) {
	if req.Since.IsZero() || req.Until.IsZero() {
		return options{}, configErr("since and until are required")
	}
	if !req.Until.After(req.Since) {
		return options{}, configErr("until must be after since")
	}
	roots, err := requiredPaths(req.Config)
	if err != nil {
		return options{}, err
	}
	authors, err := authorList(req)
	if err != nil {
		return options{}, err
	}
	return options{
		roots:   roots,
		since:   req.Since,
		until:   req.Until,
		authors: newAuthorFilter(authors),
	}, nil
}

func requiredPaths(config map[string]any) ([]string, error) {
	if config == nil {
		return nil, configErr("roots is required")
	}
	value, ok := config["roots"]
	if !ok {
		return nil, configErr("roots is required")
	}
	raw, ok := stringList(value)
	if !ok {
		return nil, configErr("roots must be an array of strings")
	}
	if len(raw) == 0 {
		return nil, configErr("roots must contain at least one path")
	}
	roots := make([]string, 0, len(raw))
	for _, root := range raw {
		expanded, err := expandPath(root)
		if err != nil {
			return nil, configErr(err.Error())
		}
		if expanded == "" {
			return nil, configErr("roots must not contain an empty path")
		}
		if strings.IndexFunc(expanded, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return nil, configErr("roots must not contain control characters")
		}
		roots = append(roots, expanded)
	}
	return roots, nil
}

func authorList(req source.CollectRequest) ([]string, error) {
	if req.Config != nil {
		if value, ok := req.Config["authors"]; ok {
			raw, ok := stringList(value)
			if !ok {
				return nil, configErr("authors must be an array of strings")
			}
			return trimList(raw), nil
		}
	}
	return trimList(req.Identity.Emails), nil
}

func trimList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func stringList(value any) ([]string, bool) {
	switch items := value.(type) {
	case []string:
		return items, true
	case []any:
		out := make([]string, len(items))
		for i, item := range items {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out[i] = s
		}
		return out, true
	default:
		return nil, false
	}
}

func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	expanded, err := expandUser(path)
	if err != nil {
		return "", err
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

func configErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrConfig}
}

func failedErr(reason string) error {
	return &source.Failure{Name: sourceName, Op: "collect", Reason: reason, Err: source.ErrFailed}
}
