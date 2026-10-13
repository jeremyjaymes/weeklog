package git

import (
	"regexp"
	"strings"
)

// refPattern finds pull-request numbers, ticket keys, and SHAs in a message.
// Ticket keys and SHAs are distinguished after the match.
var refPattern = regexp.MustCompile(`#\d+|\b[A-Za-z][A-Za-z0-9]{1,9}-\d{1,7}\b|\b[0-9a-fA-F]{7,40}\b`)

// notTickets are common tokens that look like PROJ-123 and are not tickets.
var notTickets = map[string]struct{}{
	"UTF":  {},
	"ISO":  {},
	"RFC":  {},
	"SHA":  {},
	"IEEE": {},
}

// parseRefs returns ticket keys, #numbers, and SHAs mentioned in message.
// selfSHA is the commit being parsed and is not reported as a ref.
// Ticket keys are uppercased. SHAs are lowercased. Order follows the message.
func parseRefs(message, selfSHA string) []string {
	selfSHA = strings.ToLower(strings.TrimSpace(selfSHA))
	matches := refPattern.FindAllString(message, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	var refs []string
	for _, match := range matches {
		ref, ok := canonicalRef(match, selfSHA)
		if !ok {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

func canonicalRef(match, selfSHA string) (string, bool) {
	if strings.HasPrefix(match, "#") {
		return match, true
	}
	if strings.Contains(match, "-") {
		key := strings.ToUpper(match)
		project, _, _ := strings.Cut(key, "-")
		if _, skip := notTickets[project]; skip {
			return "", false
		}
		return key, true
	}
	sha := strings.ToLower(match)
	if !hasDigit(sha) {
		return "", false
	}
	if selfSHA != "" && (sha == selfSHA || strings.HasPrefix(selfSHA, sha)) {
		return "", false
	}
	return sha, true
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
