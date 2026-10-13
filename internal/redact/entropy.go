package redact

import "math"

const (
	// entropyMinLen is the shortest token the high-entropy check considers.
	// The token must also contain no whitespace; the scanner's alphabet
	// enforces that.
	entropyMinLen = 24

	// entropyThreshold is the Shannon entropy, in bits per byte, at or above
	// which a token is treated as a secret. Hex-like strings are excluded
	// before this comparison so git SHAs are kept.
	entropyThreshold = 4.0
)

// entropyRE matches a single whitespace-free token. Hex-like tokens are
// filtered afterward.
var entropyRE = mustCompile(`[A-Za-z0-9+/_=-]{24,}`)

func shannon(s string) float64 {
	if s == "" {
		return 0
	}
	var freq [256]int
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
	}
	n := float64(len(s))
	var h float64
	for _, count := range freq {
		if count == 0 {
			continue
		}
		p := float64(count) / n
		h -= p * math.Log2(p)
	}
	return h
}

// isHexLike reports whether s is a hex string, optionally broken by '-' or '_'.
// Git SHAs and UUIDs fall in this set. Their entropy overlaps real hex-encoded
// secrets, so the entropy check leaves them to explicit patterns and assignments.
func isHexLike(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
