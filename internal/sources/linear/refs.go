package linear

import (
	"regexp"
	"strings"
)

// defaultTicketPattern is the built-in ticket key shape, the same shape the
// linker and the git source use. ISO-8601, UTF-8, RFC-123, SHA-1, and
// IEEE-802 are not treated as tickets. A configured ticket_pattern replaces
// this and is used as written.
const defaultTicketPattern = `(?i)\b[A-Za-z][A-Za-z0-9]{1,9}-\d{1,7}\b`

var defaultTicketRE = regexp.MustCompile(defaultTicketPattern)

// notTickets are common tokens that look like PROJ-123 and are not tickets.
// The denylist applies only to defaultTicketPattern.
var notTickets = map[string]struct{}{
	"UTF":  {},
	"ISO":  {},
	"RFC":  {},
	"SHA":  {},
	"IEEE": {},
}

type matcher struct {
	re   *regexp.Regexp
	deny bool
}

func compilePattern(config map[string]any) (matcher, error) {
	text, ok, err := patternText(config)
	if err != nil {
		return matcher{}, err
	}
	if !ok {
		return matcher{re: defaultTicketRE, deny: true}, nil
	}
	re, err := regexp.Compile(text)
	if err != nil {
		return matcher{}, configErr("ticket_pattern is invalid")
	}
	return matcher{re: re, deny: false}, nil
}

func patternText(config map[string]any) (string, bool, error) {
	if config == nil {
		return "", false, nil
	}
	value, ok := config["ticket_pattern"]
	if !ok || value == nil {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", false, configErr("ticket_pattern must be a string")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false, nil
	}
	return text, true, nil
}

// refs returns ticket keys found in parts, in order, without duplicates.
// With the built-in pattern, keys are uppercased and the denylist applies.
// A configured pattern keeps the matched text so the linker can match it.
func (m matcher) refs(parts ...string) []string {
	if m.re == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var refs []string
	for _, part := range parts {
		for _, match := range m.re.FindAllString(part, -1) {
			if match == "" {
				continue
			}
			ref := match
			if m.deny {
				ref = strings.ToUpper(match)
				project, _, _ := strings.Cut(ref, "-")
				if _, skip := notTickets[project]; skip {
					continue
				}
			}
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}
