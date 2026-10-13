package redact

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/jeremyjaymes/weeklog/internal/event"
)

// Options configures a Redactor. Zero values use the built-in rules only.
// Secrets are exact values, usually resolved from weeklog config. Callers
// must not log them.
type Options struct {
	ExtraPatterns []string
	Allow         []string
	Secrets       []string
}

// Redactor removes secrets from text and events. A Redactor is immutable and
// safe for concurrent use. Printing it does not reveal configured secrets.
type Redactor struct {
	allow   map[string]struct{}
	secrets []string
	rules   []rule
	extras  []rule
}

type rule struct {
	re    *regexp.Regexp
	apply func(groups []string) (replacement string, sensitive []string, ok bool)
}

var (
	defaultOnce     sync.Once
	defaultRedactor *Redactor
)

// Default is the built-in redactor. It has no config secrets, extra patterns,
// or allow list.
func Default() *Redactor {
	defaultOnce.Do(func() {
		r, err := New(Options{})
		if err != nil {
			panic("weeklog: builtin redaction rules: " + err.Error())
		}
		defaultRedactor = r
	})
	return defaultRedactor
}

// New compiles a redactor. ExtraPatterns use the same regular-expression
// syntax as config check. Allow entries are exact secret strings that should
// be left in place. Secrets are removed wherever they appear.
func New(opts Options) (*Redactor, error) {
	allow := make(map[string]struct{}, len(opts.Allow))
	for i, item := range opts.Allow {
		if item == "" {
			return nil, fmt.Errorf("redaction.allow[%d]: must not be empty", i)
		}
		allow[item] = struct{}{}
	}

	extras := make([]rule, 0, len(opts.ExtraPatterns))
	for i, pattern := range opts.ExtraPatterns {
		if pattern == "" {
			return nil, fmt.Errorf("redaction.extra_patterns[%d]: must not be empty", i)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("redaction.extra_patterns[%d]: invalid regular expression", i)
		}
		extras = append(extras, rule{
			re: re,
			apply: func(g []string) (string, []string, bool) {
				if len(g) == 0 || g[0] == "" {
					return "", nil, false
				}
				return Marker(labelCustom), []string{g[0]}, true
			},
		})
	}

	seen := make(map[string]struct{}, len(opts.Secrets))
	secrets := make([]string, 0, len(opts.Secrets))
	for _, secret := range opts.Secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		if _, skip := allow[secret]; skip {
			continue
		}
		if _, dup := seen[secret]; dup {
			continue
		}
		seen[secret] = struct{}{}
		secrets = append(secrets, secret)
	}
	sort.Slice(secrets, func(i, j int) bool {
		if len(secrets[i]) != len(secrets[j]) {
			return len(secrets[i]) > len(secrets[j])
		}
		return secrets[i] < secrets[j]
	})

	return &Redactor{
		allow:   allow,
		secrets: secrets,
		rules:   builtinRules(),
		extras:  extras,
	}, nil
}

// String reports how many secrets and exceptions the redactor holds.
// It never includes secret values.
func (r *Redactor) String() string {
	if r == nil {
		return "Redactor(nil)"
	}
	return fmt.Sprintf("Redactor(secrets=%d allow=%d extra=%d)", len(r.secrets), len(r.allow), len(r.extras))
}

// GoString is String. %#v must not reveal configured secrets.
func (r *Redactor) GoString() string { return r.String() }

// Marker is the replacement text for a removed secret of the given kind.
func Marker(kind string) string {
	if !markerKindRE.MatchString(kind) {
		kind = labelCustom
	}
	return "[REDACTED:" + kind + "]"
}

// Text redacts secrets in s. The result is safe to store and to render.
// A nil receiver uses Default.
func (r *Redactor) Text(s string) string {
	if r == nil || len(r.rules) == 0 {
		r = Default()
	}
	if s == "" || r.allowed(s) {
		return s
	}
	s = replaceConfigSecrets(s, r.secrets, r.allow)
	for i := range r.rules {
		s = applyRule(s, r.rules[i], r.allow)
	}
	for i := range r.extras {
		s = applyRule(s, r.extras[i], r.allow)
	}
	s = applyRule(s, entropyRule(), r.allow)
	return s
}

func (r *Redactor) allowed(s string) bool {
	_, ok := r.allow[s]
	return ok
}

func entropyRule() rule {
	return rule{
		re: entropyRE,
		apply: func(g []string) (string, []string, bool) {
			if len(g) == 0 || len(g[0]) < entropyMinLen || isHexLike(g[0]) {
				return "", nil, false
			}
			if shannon(g[0]) < entropyThreshold {
				return "", nil, false
			}
			return Marker(labelHighEntropy), []string{g[0]}, true
		},
	}
}

// Event returns a copy of e with every string field redacted.
// The caller's slices and maps are not modified. Kind and State keep a known
// value when one remains after the secret is removed; a State that would no
// longer fit the schema is cleared.
func (r *Redactor) Event(e event.Event) event.Event {
	if r == nil {
		r = Default()
	}
	out := e
	out.Source = r.Text(e.Source)
	out.SourceID = r.Text(e.SourceID)
	out.Kind = redactKind(e.Kind, r.Text(string(e.Kind)))
	if e.TSEnd != nil {
		end := *e.TSEnd
		out.TSEnd = &end
	}
	out.Actor = r.Text(e.Actor)
	out.Title = r.Text(e.Title)
	out.Summary = r.Text(e.Summary)
	out.URL = r.Text(e.URL)
	out.Repo = r.Text(e.Repo)
	out.Branch = r.Text(e.Branch)
	out.CWD = r.Text(e.CWD)
	out.Files = r.texts(e.Files)
	out.Refs = r.texts(e.Refs)
	out.State = redactState(r.Text(string(e.State)))
	out.Metrics = r.metricKeys(e.Metrics)
	out.BodyRef = r.Text(e.BodyRef)
	return out
}

func (r *Redactor) texts(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = r.Text(s)
	}
	return out
}

func (r *Redactor) metricKeys(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		next := r.Text(key)
		if next == "" {
			next = Marker(labelHighEntropy)
		}
		if _, exists := out[next]; exists {
			continue
		}
		out[next] = value
	}
	return out
}

func redactKind(original event.Kind, redacted string) event.Kind {
	if !strings.Contains(redacted, "[REDACTED:") {
		return original
	}
	cleaned := strings.TrimSpace(markerRE.ReplaceAllString(redacted, " "))
	for _, field := range strings.Fields(cleaned) {
		k := event.Kind(field)
		if k.Known() {
			return k
		}
	}
	next := event.Kind(strings.TrimSpace(redacted))
	if next == "" {
		return original
	}
	return next
}

func redactState(redacted string) event.State {
	next := event.State(strings.TrimSpace(redacted))
	if next == "" || next.Known() {
		return next
	}
	if !strings.Contains(redacted, "[REDACTED:") {
		return next
	}
	cleaned := strings.TrimSpace(markerRE.ReplaceAllString(redacted, " "))
	for _, field := range strings.Fields(cleaned) {
		st := event.State(field)
		if st.Known() {
			return st
		}
	}
	if event.State(cleaned).Known() {
		return event.State(cleaned)
	}
	return ""
}

func applyRule(s string, rl rule, allow map[string]struct{}) string {
	indexes := rl.re.FindAllStringSubmatchIndex(s, -1)
	if len(indexes) == 0 {
		return s
	}
	groups := rl.re.FindAllStringSubmatch(s, -1)
	if len(groups) != len(indexes) {
		return s
	}
	spans := markerSpans(s)
	var b strings.Builder
	b.Grow(len(s) + 16)
	prev := 0
	for i, loc := range indexes {
		start, end := loc[0], loc[1]
		if start < prev || end <= start {
			continue
		}
		if overlaps(spans, start, end) {
			continue
		}
		full := s[start:end]
		if strings.Contains(full, "[REDACTED:") {
			continue
		}
		replacement, sensitive, ok := rl.apply(groups[i])
		if !ok || replacement == "" {
			continue
		}
		if allowed(allow, full, sensitive) {
			continue
		}
		b.WriteString(s[prev:start])
		b.WriteString(replacement)
		prev = end
	}
	if prev == 0 {
		return s
	}
	b.WriteString(s[prev:])
	return b.String()
}

func allowed(allow map[string]struct{}, full string, sensitive []string) bool {
	if len(allow) == 0 {
		return false
	}
	if _, ok := allow[full]; ok {
		return true
	}
	for _, part := range sensitive {
		if part == "" {
			continue
		}
		if _, ok := allow[part]; ok {
			return true
		}
	}
	return false
}

func markerSpans(s string) [][2]int {
	locs := markerRE.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return nil
	}
	spans := make([][2]int, len(locs))
	for i, loc := range locs {
		spans[i] = [2]int{loc[0], loc[1]}
	}
	return spans
}

func overlaps(spans [][2]int, start, end int) bool {
	for _, span := range spans {
		if start < span[1] && end > span[0] {
			return true
		}
	}
	return false
}

func markerLen(s string) (int, bool) {
	if !strings.HasPrefix(s, "[REDACTED:") {
		return 0, false
	}
	rel := strings.IndexByte(s, ']')
	if rel < 0 {
		return 0, false
	}
	inner := s[len("[REDACTED:"):rel]
	if !markerKindRE.MatchString(inner) {
		return 0, false
	}
	return rel + 1, true
}
