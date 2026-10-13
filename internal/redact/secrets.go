package redact

import (
	"strings"
	"unicode/utf8"
)

// minInlineSecretLen is the shortest config secret that is removed even when
// it sits inside a longer word. Shorter values are removed only as whole
// tokens, bounded by a non-alphanumeric character or the ends of the string.
const minInlineSecretLen = 8

// replaceConfigSecrets removes exact secret values. secrets is longest-first.
// Replacements are not scanned again.
func replaceConfigSecrets(s string, secrets []string, allow map[string]struct{}) string {
	if s == "" || len(secrets) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if n, ok := markerLen(s[i:]); ok {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		matched := false
		for _, secret := range secrets {
			if secret == "" || i+len(secret) > len(s) || s[i:i+len(secret)] != secret {
				continue
			}
			if _, skip := allow[secret]; skip {
				continue
			}
			if !secretBoundary(s, i, len(secret)) {
				continue
			}
			b.WriteString(Marker(labelConfig))
			i += len(secret)
			matched = true
			break
		}
		if matched {
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func secretBoundary(s string, start, n int) bool {
	if n >= minInlineSecretLen {
		return true
	}
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:start])
		if isWord(r) {
			return false
		}
	}
	end := start + n
	if end < len(s) {
		r, _ := utf8.DecodeRuneInString(s[end:])
		if isWord(r) {
			return false
		}
	}
	return true
}

func isWord(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
