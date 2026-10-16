package config

import (
	"regexp"
	"strings"
)

// Builtin source schemas mirror the describe manifests of the built-in
// sources. Unknown source types are external plugins: their keys are
// preserved, and secret-like keys are resolved. config check does not
// execute plugins.
type fieldKind int

const (
	fieldString fieldKind = iota
	fieldStrings
	fieldSecret
)

type fieldSpec struct {
	Kind     fieldKind
	Required bool
}

type sourceSchema struct {
	Fields map[string]fieldSpec
}

func builtinSchemas() map[string]sourceSchema {
	return map[string]sourceSchema{
		"git": {
			Fields: map[string]fieldSpec{
				"roots":   {Kind: fieldStrings, Required: true},
				"authors": {Kind: fieldStrings},
			},
		},
		"claude-code": {Fields: map[string]fieldSpec{}},
		"codex":       {Fields: map[string]fieldSpec{}},
		"github": {
			Fields: map[string]fieldSpec{
				"token": {Kind: fieldSecret},
			},
		},
		"linear": {
			Fields: map[string]fieldSpec{
				"token":          {Kind: fieldSecret, Required: true},
				"ticket_pattern": {Kind: fieldString},
			},
		},
		"cursor-cloud": {
			Fields: map[string]fieldSpec{
				"token": {Kind: fieldSecret, Required: true},
			},
		},
	}
}

var sourceTypePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// canonicalSchema reports the built-in schema for sourceType.
// exact is false when sourceType matches a built-in name with different case.
func canonicalSchema(sourceType string) (canonical string, schema sourceSchema, exact bool, found bool) {
	schemas := builtinSchemas()
	if s, ok := schemas[sourceType]; ok {
		return sourceType, s, true, true
	}
	for name, s := range schemas {
		if strings.EqualFold(name, sourceType) {
			return name, s, false, true
		}
	}
	return "", sourceSchema{}, false, false
}

// isSecretKey reports whether a free-form source key should be treated as a
// secret. Built-in schemas mark their own secret fields explicitly; this
// covers external plugins.
func isSecretKey(key string) bool {
	k := normalizeKey(key)
	switch k {
	case "token", "secret", "password", "passwd", "api_key", "apikey":
		return true
	}
	return hasSuffix(k, "_token") || hasSuffix(k, "_secret") || hasSuffix(k, "_password") || hasSuffix(k, "_key")
}

func normalizeKey(key string) string {
	b := make([]byte, len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c == '-' {
			c = '_'
		}
		b[i] = c
	}
	return string(b)
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
