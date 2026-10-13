package redact

import (
	"regexp"
	"strings"
)

const (
	labelAWSAccess   = "aws-access-key"
	labelAWSSecret   = "aws-secret"
	labelGitHub      = "github-token"
	labelSlack       = "slack-token"
	labelStripe      = "stripe-key"
	labelOpenAI      = "openai-key"
	labelAnthropic   = "anthropic-key"
	labelPrivateKey  = "private-key"
	labelJWT         = "jwt"
	labelBearer      = "bearer-token"
	labelURL         = "url-credential"
	labelPassword    = "password"
	labelToken       = "token"
	labelSecret      = "secret"
	labelAPIKey      = "api-key"
	labelPrivate     = "private"
	labelHighEntropy = "high-entropy"
	labelConfig      = "config-secret"
	labelCustom      = "custom"
)

// markerKindRE matches the label inside a replacement marker.
var markerKindRE = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

var markerRE = regexp.MustCompile(`\[REDACTED:[a-z0-9-]+\]`)

func mustCompile(expr string) *regexp.Regexp {
	return regexp.MustCompile(expr)
}

// builtinRules are applied in order. More specific shapes come first so a
// GitHub token inside other text keeps the github-token label.
func builtinRules() []rule {
	return []rule{
		simple(`-----BEGIN [A-Z0-9 ]{0,80}PRIVATE KEY(?: BLOCK)?-----[\s\S]*?-----END [A-Z0-9 ]{0,80}PRIVATE KEY(?: BLOCK)?-----`, labelPrivateKey),
		simple(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`, labelJWT),
		simple(`github_pat_[A-Za-z0-9_]{82,}`, labelGitHub),
		simple(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}`, labelGitHub),
		simple(`(?:AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA|A3T[A-Z0-9])[A-Z0-9]{16}`, labelAWSAccess),
		{
			re: mustCompile(`(?i)\b(?:aws_secret_access_key|aws_secret_key|secret_access_key)\s*=\s*['"]?([A-Za-z0-9/+=]{40})['"]?`),
			apply: func(g []string) (string, []string, bool) {
				if len(g) < 2 || len(g[1]) != 40 {
					return "", nil, false
				}
				return Marker(labelAWSSecret), []string{g[1]}, true
			},
		},
		simple(`https://hooks\.slack\.com/services/T[A-Z0-9]{8,}/B[A-Z0-9]{8,}/[A-Za-z0-9]{16,}`, labelSlack),
		simple(`xox[baprs]-[A-Za-z0-9-]{10,}`, labelSlack),
		simple(`(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{16,}`, labelStripe),
		simple(`sk-ant-[A-Za-z0-9_-]{20,}`, labelAnthropic),
		simple(`sk-(?:proj-)?[A-Za-z0-9_-]{20,}`, labelOpenAI),
		{
			re: mustCompile(`(?i)Bearer[ \t]+([A-Za-z0-9\-._~+/=]{16,})`),
			apply: func(g []string) (string, []string, bool) {
				if len(g) < 2 || g[1] == "" {
					return "", nil, false
				}
				return Marker(labelBearer), []string{g[1]}, true
			},
		},
		{
			re: mustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)([^/\s:@]+):([^/\s@]+)@`),
			apply: func(g []string) (string, []string, bool) {
				if len(g) < 4 || g[2] == "" || g[3] == "" {
					return "", nil, false
				}
				return g[1] + Marker(labelURL) + "@", []string{g[3], g[2] + ":" + g[3]}, true
			},
		},
		assignmentEquals(mustCompile(`(?i)(^|[^A-Za-z0-9])([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=;&,.)]+))`)),
		assignmentJSON(mustCompile(`(?i)"([^"]*(?:secret|token|password|passwd|api[_-]?key|private)[^"]*)"\s*:\s*"([^"]*)"`)),
		assignmentYAML(mustCompile(`(?i)(^|[^A-Za-z0-9])([A-Za-z_][A-Za-z0-9_.-]*)\s*:\s*([^"'\[{\s#][^\s#,.)]{3,})`)),
	}
}

func simple(expr, label string) rule {
	return rule{
		re: mustCompile(expr),
		apply: func(g []string) (string, []string, bool) {
			if len(g) == 0 || g[0] == "" {
				return "", nil, false
			}
			return Marker(label), []string{g[0]}, true
		},
	}
}

// assignmentKey matches the plan's KEY=value key shape.
var assignmentKey = regexp.MustCompile(`(?i)api[_-]?key|passwd|password|token|secret|private`)

func assignmentEquals(re *regexp.Regexp) rule {
	return rule{
		re: re,
		apply: func(g []string) (string, []string, bool) {
			// groups: full, prefix, key, double-quoted, single-quoted, bare
			if len(g) < 6 || !assignmentKey.MatchString(g[2]) {
				return "", nil, false
			}
			value := firstNonEmpty(g[3], g[4], g[5])
			if value == "" || markerRE.MatchString(value) {
				return "", nil, false
			}
			return g[1] + g[2] + "=" + Marker(labelForKey(g[2])), []string{value}, true
		},
	}
}

func assignmentJSON(re *regexp.Regexp) rule {
	return rule{
		re: re,
		apply: func(g []string) (string, []string, bool) {
			if len(g) < 3 || g[2] == "" || markerRE.MatchString(g[2]) || !assignmentKey.MatchString(g[1]) {
				return "", nil, false
			}
			return `"` + g[1] + `": "` + Marker(labelForKey(g[1])) + `"`, []string{g[2]}, true
		},
	}
}

func assignmentYAML(re *regexp.Regexp) rule {
	return rule{
		re: re,
		apply: func(g []string) (string, []string, bool) {
			if len(g) < 4 || g[3] == "" || len(g[3]) < 4 || markerRE.MatchString(g[3]) || !assignmentKey.MatchString(g[2]) {
				return "", nil, false
			}
			return g[1] + g[2] + ": " + Marker(labelForKey(g[2])), []string{g[3]}, true
		},
	}
}

func labelForKey(key string) string {
	k := strings.ToLower(key)
	switch {
	case strings.Contains(k, "api_key"), strings.Contains(k, "api-key"), strings.Contains(k, "apikey"):
		return labelAPIKey
	case strings.Contains(k, "passwd"), strings.Contains(k, "password"):
		return labelPassword
	case strings.Contains(k, "token"):
		return labelToken
	case strings.Contains(k, "secret"):
		return labelSecret
	case strings.Contains(k, "private"):
		return labelPrivate
	default:
		return labelSecret
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
