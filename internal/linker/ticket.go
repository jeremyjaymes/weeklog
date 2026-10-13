package linker

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jeremyjaymes/weeklog/internal/config"
)

// DefaultTicketPattern is the built-in ticket key shape, the same shape the
// git source extracts. It is used when no source configures ticket_pattern.
// ISO-8601, UTF-8, RFC-123, SHA-1, and IEEE-802 are not treated as tickets.
const DefaultTicketPattern = `(?i)\b[A-Za-z][A-Za-z0-9]{1,9}-\d{1,7}\b`

var defaultTicketRE = regexp.MustCompile(DefaultTicketPattern)

// boringProjects look like ticket keys and are not tickets. The denylist
// applies only to DefaultTicketPattern. A configured ticket_pattern is used
// as written.
var boringProjects = map[string]struct{}{
	"UTF":  {},
	"ISO":  {},
	"RFC":  {},
	"SHA":  {},
	"IEEE": {},
}

// TicketPatterns returns the ticket_pattern values configured on sources,
// in source order, without duplicates. Nil means no source set one.
func TicketPatterns(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	seen := make(map[string]struct{})
	for _, src := range cfg.Sources {
		value, ok := src.Settings["ticket_pattern"]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	return out
}

type ticketMatcher struct {
	res  []*regexp.Regexp
	deny bool
}

func compilePatterns(patterns []string) (ticketMatcher, error) {
	if patterns == nil {
		return ticketMatcher{res: []*regexp.Regexp{defaultTicketRE}, deny: true}, nil
	}
	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return ticketMatcher{}, fmt.Errorf("ticket_pattern %q: %w", pattern, err)
		}
		res = append(res, re)
	}
	return ticketMatcher{res: res, deny: false}, nil
}

// keys returns ticket keys found in text, uppercased, in appearance order.
func (m ticketMatcher) keys(text string) []string {
	if text == "" || len(m.res) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, re := range m.res {
		for _, match := range re.FindAllString(text, -1) {
			key := strings.ToUpper(match)
			if m.deny && boringKey(key) {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	return out
}

func boringKey(key string) bool {
	project, _, ok := strings.Cut(key, "-")
	if !ok {
		return false
	}
	_, skip := boringProjects[project]
	return skip
}
