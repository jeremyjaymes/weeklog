package git

import "strings"

// authorFilter selects commits. An inactive filter keeps every author.
// Emails and names are compared case-insensitively.
type authorFilter struct {
	active bool
	emails map[string]struct{}
	names  map[string]struct{}
}

func newAuthorFilter(authors []string) authorFilter {
	filter := authorFilter{
		emails: make(map[string]struct{}),
		names:  make(map[string]struct{}),
	}
	for _, author := range authors {
		author = strings.TrimSpace(author)
		if author == "" {
			continue
		}
		if email, ok := angleEmail(author); ok {
			filter.emails[strings.ToLower(email)] = struct{}{}
			continue
		}
		if strings.Contains(author, "@") {
			filter.emails[strings.ToLower(author)] = struct{}{}
			continue
		}
		filter.names[strings.ToLower(author)] = struct{}{}
	}
	filter.active = len(filter.emails) > 0 || len(filter.names) > 0
	return filter
}

func (f authorFilter) allow(name, email string) bool {
	if !f.active {
		return true
	}
	if _, ok := f.emails[strings.ToLower(strings.TrimSpace(email))]; ok {
		return true
	}
	if _, ok := f.names[strings.ToLower(strings.TrimSpace(name))]; ok {
		return true
	}
	return false
}

func angleEmail(s string) (string, bool) {
	start := strings.LastIndex(s, "<")
	end := strings.LastIndex(s, ">")
	if start < 0 || end <= start+1 {
		return "", false
	}
	email := strings.TrimSpace(s[start+1 : end])
	if email == "" || strings.ContainsAny(email, " \t") || !strings.Contains(email, "@") {
		return "", false
	}
	return email, true
}
