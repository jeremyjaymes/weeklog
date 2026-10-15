package config

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Severity is the level of a config check finding.
type Severity string

const (
	// SeverityError means config check failed.
	SeverityError Severity = "error"
	// SeverityWarning means config check can still succeed.
	SeverityWarning Severity = "warning"
)

// Issue is one config check finding. Messages never include secret values.
type Issue struct {
	Severity Severity
	Field    string
	Message  string
}

// CheckResult is the outcome of Check.
// Formatting or marshaling a result never includes resolved secret values.
type CheckResult struct {
	Path    string
	Config  *Config
	Issues  []Issue
	secrets []resolvedSecret
}

type resolvedSecret struct {
	SourceIndex int
	Key         string
	Display     string
	Literal     bool
	value       secretValue
	err         error
}

// Check loads path, validates it, and resolves secret references.
// A non-nil error means the file could not be read or parsed.
// Validation and resolution problems are Issues on the result.
func Check(ctx context.Context, path string) (CheckResult, error) {
	cfg, err := Load(path)
	if err != nil {
		return CheckResult{}, err
	}
	result := CheckResult{
		Path:   cfg.Path,
		Config: cfg,
	}
	result.Issues = append(result.Issues, validate(cfg)...)
	secrets, issues := resolveSecrets(ctx, cfg)
	result.secrets = secrets
	result.Issues = append(result.Issues, issues...)
	redactLiteralSettings(cfg, secrets)
	return result, nil
}

// OK reports whether the result has no errors. Warnings still return true.
func (r CheckResult) OK() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Secret returns a resolved secret for a source field.
// The value may be a literal token. Callers must not log it.
func (r CheckResult) Secret(sourceIndex int, key string) (string, bool) {
	for _, secret := range r.secrets {
		if secret.SourceIndex == sourceIndex && secret.Key == key && secret.err == nil {
			return secret.value.Value(), true
		}
	}
	return "", false
}

// PluginConfig returns a copy of source index's settings with secret fields
// replaced by their resolved values. The map contains secrets. Callers must
// not log or display it. ok is false when index is outside the loaded config.
func (r CheckResult) PluginConfig(index int) (map[string]any, bool) {
	if r.Config == nil || index < 0 || index >= len(r.Config.Sources) {
		return nil, false
	}
	settings := r.Config.Sources[index].Settings
	out := make(map[string]any, len(settings))
	for key, value := range settings {
		out[key] = value
	}
	for _, secret := range r.secrets {
		if secret.SourceIndex != index || secret.err != nil {
			continue
		}
		out[secret.Key] = secret.value.Value()
	}
	return out, true
}

// SecretValues returns resolved secret values, including literals.
// Unresolved references are omitted. Values are unique and follow first
// appearance. Callers must not log or display the result.
func (r CheckResult) SecretValues() []string {
	if len(r.secrets) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(r.secrets))
	out := make([]string, 0, len(r.secrets))
	for _, secret := range r.secrets {
		if secret.err != nil {
			continue
		}
		value := secret.value.Value()
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Report returns the human-readable check output.
// It says whether each secret resolved and never prints the value.
func (r CheckResult) Report() string {
	var b strings.Builder
	path := r.Path
	if r.Config == nil {
		fmt.Fprintf(&b, "config: %s\n", oneLine(path))
		fmt.Fprintf(&b, "%s\n", summary(r.Issues))
		return b.String()
	}
	cfg := r.Config
	fmt.Fprintf(&b, "config: %s\n", oneLine(path))
	fmt.Fprintf(&b, "identity.timezone: %s\n", displayTimezone(cfg.Identity.Timezone))
	fmt.Fprintf(&b, "identity.week_starts: %s\n", displayOr(cfg.Identity.WeekStarts, "monday"))
	fmt.Fprintf(&b, "identity.github: %s\n", displayOr(cfg.Identity.GitHub, "unset"))
	fmt.Fprintf(&b, "identity.emails: %s\n", displayEmails(cfg.Identity.Emails))
	fmt.Fprintf(&b, "output.dir: %s\n", displayOr(cfg.Output.Dir, "unset"))
	fmt.Fprintf(&b, "output.template: %s\n", displayOr(cfg.Output.Template, "built-in"))
	if len(cfg.Redaction.ExtraPatterns) > 0 || len(cfg.Redaction.Allow) > 0 {
		fmt.Fprintf(&b, "redaction.extra_patterns: %d\n", len(cfg.Redaction.ExtraPatterns))
		fmt.Fprintf(&b, "redaction.allow: %d\n", len(cfg.Redaction.Allow))
	}
	for i, src := range cfg.Sources {
		fmt.Fprintf(&b, "source[%d] %s\n", i, displayOr(src.Type, "-"))
		for _, secret := range secretsFor(r.secrets, i) {
			fmt.Fprintf(&b, "  %s: %s\n", oneLine(secret.Key), secret.status())
		}
	}
	for _, issue := range r.Issues {
		fmt.Fprintf(&b, "%s: %s: %s\n", issue.Severity, oneLine(issue.Field), oneLine(issue.Message))
	}
	fmt.Fprintf(&b, "%s\n", summary(r.Issues))
	return b.String()
}

func (r CheckResult) String() string { return r.Report() }

func (r CheckResult) Format(f fmt.State, verb rune) {
	text := r.Report()
	if verb == 'q' {
		_, _ = fmt.Fprintf(f, "%q", text)
		return
	}
	_, _ = io.WriteString(f, text)
}

func (s resolvedSecret) status() string {
	if s.err != nil {
		return fmt.Sprintf("unresolved (%s): %s", s.Display, oneLine(s.err.Error()))
	}
	if s.Literal {
		return "literal"
	}
	return fmt.Sprintf("resolved (%s)", s.Display)
}

func secretsFor(secrets []resolvedSecret, index int) []resolvedSecret {
	var out []resolvedSecret
	for _, secret := range secrets {
		if secret.SourceIndex == index {
			out = append(out, secret)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func redactLiteralSettings(cfg *Config, secrets []resolvedSecret) {
	for _, secret := range secrets {
		if !secret.Literal {
			continue
		}
		if secret.SourceIndex < 0 || secret.SourceIndex >= len(cfg.Sources) {
			continue
		}
		cfg.Sources[secret.SourceIndex].Settings[secret.Key] = "[literal]"
	}
}

func summary(issues []Issue) string {
	errors, warnings := 0, 0
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		}
	}
	switch {
	case errors == 0 && warnings == 0:
		return "ok"
	case errors == 0:
		return plural(warnings, "warning")
	case warnings == 0:
		return plural(errors, "error")
	default:
		return plural(errors, "error") + ", " + plural(warnings, "warning")
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func displayTimezone(tz string) string {
	if tz == "" || strings.EqualFold(tz, "local") {
		return "local"
	}
	return oneLine(tz)
}

func displayOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return oneLine(value)
}

func displayEmails(emails []string) string {
	if len(emails) == 0 {
		return "unset"
	}
	parts := make([]string, len(emails))
	for i, email := range emails {
		parts[i] = oneLine(email)
	}
	return strings.Join(parts, ", ")
}

func validate(cfg *Config) []Issue {
	var issues []Issue
	issues = append(issues, validateIdentity(cfg.Identity)...)
	issues = append(issues, validateOutput(cfg.Output)...)
	issues = append(issues, validateRedaction(cfg.Redaction)...)
	if len(cfg.Sources) == 0 {
		issues = append(issues, warn("sources", "no sources configured"))
	}
	for i := range cfg.Sources {
		issues = append(issues, validateSource(i, &cfg.Sources[i])...)
	}
	return issues
}

func validateIdentity(id Identity) []Issue {
	var issues []Issue
	if _, err := id.Location(); err != nil {
		issues = append(issues, errIssue("identity.timezone", err.Error()))
	}
	if _, err := id.WeekStart(); err != nil {
		issues = append(issues, errIssue("identity.week_starts", err.Error()))
	}
	for i, email := range id.Emails {
		field := fmt.Sprintf("identity.emails[%d]", i)
		if email == "" {
			issues = append(issues, errIssue(field, "must not be empty"))
			continue
		}
		if strings.ContainsAny(email, " \t\r\n") || containsControl(email) {
			issues = append(issues, errIssue(field, "must not contain whitespace"))
		}
	}
	if strings.ContainsAny(id.GitHub, " \t\r\n") || containsControl(id.GitHub) {
		issues = append(issues, errIssue("identity.github", "must not contain whitespace"))
	}
	return issues
}

func validateOutput(out Output) []Issue {
	if strings.TrimSpace(out.Dir) == "" {
		return []Issue{warn("output.dir", "is unset")}
	}
	return nil
}

func validateRedaction(red Redaction) []Issue {
	var issues []Issue
	for i, pattern := range red.ExtraPatterns {
		field := fmt.Sprintf("redaction.extra_patterns[%d]", i)
		if pattern == "" {
			issues = append(issues, errIssue(field, "must not be empty"))
			continue
		}
		if _, err := regexp.Compile(pattern); err != nil {
			issues = append(issues, errIssue(field, "invalid regular expression"))
		}
	}
	for i, item := range red.Allow {
		if item == "" {
			issues = append(issues, errIssue(fmt.Sprintf("redaction.allow[%d]", i), "must not be empty"))
		}
	}
	return issues
}

func validateSource(index int, src *Source) []Issue {
	field := fmt.Sprintf("source[%d].type", index)
	if src.Type == "" {
		return []Issue{errIssue(field, "required field is missing")}
	}
	if !sourceTypePattern.MatchString(src.Type) {
		return []Issue{errIssue(field, "invalid source type "+quoteUser(src.Type))}
	}
	canonical, schema, exact, found := canonicalSchema(src.Type)
	if found && !exact {
		return []Issue{errIssue(field, "source type "+quoteUser(src.Type)+" must be "+quoteUser(canonical))}
	}
	if !found {
		return validateExternalSecrets(index, src)
	}
	return validateBuiltinFields(index, canonical, schema, src)
}

func validateBuiltinFields(index int, canonical string, schema sourceSchema, src *Source) []Issue {
	var issues []Issue
	names := make([]string, 0, len(schema.Fields))
	for name := range schema.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec := schema.Fields[name]
		field := fmt.Sprintf("source[%d].%s", index, name)
		value, ok := src.Settings[name]
		if !ok {
			if spec.Required {
				issues = append(issues, errIssue(field, "required field is missing"))
			}
			continue
		}
		switch spec.Kind {
		case fieldString, fieldSecret:
			text, isString := value.(string)
			if !isString {
				issues = append(issues, errIssue(field, "expected a string"))
				continue
			}
			if spec.Kind == fieldString && strings.TrimSpace(text) == "" {
				issues = append(issues, errIssue(field, "must not be empty"))
			}
			if name == "ticket_pattern" && isString && strings.TrimSpace(text) != "" {
				if _, err := regexp.Compile(text); err != nil {
					issues = append(issues, errIssue(field, "invalid regular expression"))
				}
			}
		case fieldStrings:
			items, isList := value.([]string)
			if !isList {
				issues = append(issues, errIssue(field, "expected an array of strings"))
				continue
			}
			if spec.Required && len(items) == 0 {
				issues = append(issues, errIssue(field, "must contain at least one value"))
			}
			for i, item := range items {
				if strings.TrimSpace(item) == "" {
					issues = append(issues, errIssue(fmt.Sprintf("%s[%d]", field, i), "must not be empty"))
				}
			}
		}
	}
	var unknown []string
	for key := range src.Settings {
		if _, ok := schema.Fields[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		field := fmt.Sprintf("source[%d].%s", index, key)
		if loopbackEndpoint(canonical, key, src.Settings[key]) {
			continue
		}
		if key == "endpoint" && (canonical == "github" || canonical == "linear") {
			issues = append(issues, errIssue(field, "must be an http URL on 127.0.0.1, localhost, or ::1"))
			continue
		}
		issues = append(issues, errIssue(field, "unknown field for "+canonical))
	}
	return issues
}

// loopbackEndpoint reports whether key is the github or linear test override
// and value is an http URL on the loopback interface. The key is not part of
// the describe schema. Collect honors the same shape and ignores anything else.
func loopbackEndpoint(canonical, key string, value any) bool {
	if key != "endpoint" || (canonical != "github" && canonical != "linear") {
		return false
	}
	_, ok := source.LoopbackEndpoint(map[string]any{"endpoint": value})
	return ok
}

func validateExternalSecrets(index int, src *Source) []Issue {
	var issues []Issue
	for _, key := range sortedSettingKeys(src.Settings) {
		if !isSecretKey(key) {
			continue
		}
		if _, ok := src.Settings[key].(string); !ok {
			issues = append(issues, errIssue(fmt.Sprintf("source[%d].%s", index, key), "expected a string"))
		}
	}
	return issues
}

func resolveSecrets(ctx context.Context, cfg *Config) ([]resolvedSecret, []Issue) {
	var secrets []resolvedSecret
	var issues []Issue
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		for _, key := range secretKeys(src) {
			field := fmt.Sprintf("source[%d].%s", i, key)
			value, ok := src.Settings[key].(string)
			if !ok {
				continue
			}
			if strings.TrimSpace(value) == "" {
				issues = append(issues, errIssue(field, "must not be empty"))
				continue
			}
			ref, err := parseSecretRef(value)
			secret := resolvedSecret{
				SourceIndex: i,
				Key:         key,
				Display:     ref.Display,
				Literal:     ref.Kind == refLiteral,
			}
			if err != nil {
				secret.err = err
				secrets = append(secrets, secret)
				issues = append(issues, errIssue(field, err.Error()))
				continue
			}
			if ref.Kind == refLiteral {
				secret.value = secretValue{s: strings.TrimSpace(value)}
				secrets = append(secrets, secret)
				issues = append(issues, warn(field, "literal secret; use an env:, cmd:, or file: reference"))
				continue
			}
			resolved, err := resolveSecretRef(ctx, ref, cfg.Dir)
			if err != nil {
				secret.err = err
				secrets = append(secrets, secret)
				issues = append(issues, errIssue(field, err.Error()))
				continue
			}
			secret.value = secretValue{s: resolved}
			secrets = append(secrets, secret)
		}
	}
	return secrets, issues
}

func secretKeys(src *Source) []string {
	if src.Type == "" || !sourceTypePattern.MatchString(src.Type) {
		return nil
	}
	_, schema, exact, found := canonicalSchema(src.Type)
	if found && exact {
		var keys []string
		for key, spec := range schema.Fields {
			if spec.Kind != fieldSecret {
				continue
			}
			if _, ok := src.Settings[key]; !ok {
				continue
			}
			if _, ok := src.Settings[key].(string); !ok {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	}
	if found && !exact {
		return nil
	}
	var keys []string
	for _, key := range sortedSettingKeys(src.Settings) {
		if !isSecretKey(key) {
			continue
		}
		if _, ok := src.Settings[key].(string); !ok {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func sortedSettingKeys(settings map[string]any) []string {
	keys := make([]string, 0, len(settings))
	for key := range settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func errIssue(field, message string) Issue {
	return Issue{Severity: SeverityError, Field: field, Message: message}
}

func warn(field, message string) Issue {
	return Issue{Severity: SeverityWarning, Field: field, Message: message}
}

func containsControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cc, r) {
			return true
		}
	}
	return false
}
