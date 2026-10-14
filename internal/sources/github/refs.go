package github

import (
	"regexp"
	"strings"
)

// textRefPattern finds pull-request numbers and ticket keys in free text.
// Commit SHAs are taken from the pull request's commit list, not from prose.
var textRefPattern = regexp.MustCompile(`#\d+|\b[A-Za-z][A-Za-z0-9]{1,9}-\d{1,7}\b`)

// notTickets are common tokens that look like PROJ-123 and are not tickets.
var notTickets = map[string]struct{}{
	"UTF":  {},
	"ISO":  {},
	"RFC":  {},
	"SHA":  {},
	"IEEE": {},
}

func textRefs(texts ...string) []string {
	seen := map[string]struct{}{}
	var refs []string
	for _, text := range texts {
		for _, match := range textRefPattern.FindAllString(text, -1) {
			ref := match
			if strings.Contains(match, "-") {
				key := strings.ToUpper(match)
				project, _, _ := strings.Cut(key, "-")
				if _, skip := notTickets[project]; skip {
					continue
				}
				ref = key
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
