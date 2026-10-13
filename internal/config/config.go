package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// maxConfigBytes limits the config file size.
var maxConfigBytes int64 = 1 << 20

// Config is a loaded weeklog configuration.
// Path fields with a leading ~ are expanded. Secret references are not resolved;
// use Check for that. Load preserves literal secret text from the file, so
// callers must not log source settings that hold secrets.
type Config struct {
	// Path is the config file that was loaded.
	Path string
	// Dir is the directory containing Path. Relative file: secrets resolve against it.
	Dir       string
	Identity  Identity
	Output    Output
	Redaction Redaction
	Sources   []Source
}

// Identity is the developer's identity and week settings.
type Identity struct {
	Emails     []string
	GitHub     string
	Timezone   string
	WeekStarts string
}

// Output controls where weekly logs are written.
type Output struct {
	Dir      string
	Template string
}

// Redaction lists extra redaction patterns and allow-list exceptions.
// config check compiles ExtraPatterns. The redact package applies both lists,
// plus resolved secret values, before events are written.
type Redaction struct {
	ExtraPatterns []string
	Allow         []string
}

// Source is one configured collector. Settings holds the keys other than type.
// String arrays from TOML are stored as []string. Secret references are stored
// as written until Check resolves them.
type Source struct {
	Type     string
	Settings map[string]any
}

type document struct {
	Identity  map[string]any   `toml:"identity"`
	Output    map[string]any   `toml:"output"`
	Redaction map[string]any   `toml:"redaction"`
	Source    []map[string]any `toml:"source"`
}

// Load reads and parses the TOML file at path.
func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("config path is empty")
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("config path is a directory: %s", path)
	}
	if info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("config file exceeds %d bytes", maxConfigBytes)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	closeErr := f.Close()
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("read config: %w", closeErr)
	}
	if int64(len(data)) > maxConfigBytes {
		return nil, fmt.Errorf("config file exceeds %d bytes", maxConfigBytes)
	}

	var doc document
	meta, err := toml.Decode(string(data), &doc)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg := &Config{
		Path: path,
		Dir:  filepath.Dir(path),
	}
	var unknown []string
	for _, key := range meta.Undecoded() {
		unknown = append(unknown, key.String())
	}

	identity, identityUnknown, err := decodeIdentity(doc.Identity)
	if err != nil {
		return nil, err
	}
	output, outputUnknown, err := decodeOutput(doc.Output)
	if err != nil {
		return nil, err
	}
	redaction, redactionUnknown, err := decodeRedaction(doc.Redaction)
	if err != nil {
		return nil, err
	}
	sources, err := decodeSources(doc.Source)
	if err != nil {
		return nil, err
	}

	unknown = append(unknown, identityUnknown...)
	unknown = append(unknown, outputUnknown...)
	unknown = append(unknown, redactionUnknown...)
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown config keys: %s", strings.Join(unknown, ", "))
	}

	cfg.Identity = identity
	cfg.Output = output
	cfg.Redaction = redaction
	cfg.Sources = sources
	return cfg, nil
}

func decodeIdentity(m map[string]any) (Identity, []string, error) {
	id := Identity{}
	if m == nil {
		id.WeekStarts = "monday"
		return id, nil, nil
	}
	var err error
	if id.Emails, err = takeStrings(m, "emails"); err != nil {
		return Identity{}, nil, fmt.Errorf("identity.emails: %w", err)
	}
	if id.GitHub, err = takeString(m, "github"); err != nil {
		return Identity{}, nil, fmt.Errorf("identity.github: %w", err)
	}
	if id.Timezone, err = takeString(m, "timezone"); err != nil {
		return Identity{}, nil, fmt.Errorf("identity.timezone: %w", err)
	}
	if id.WeekStarts, err = takeString(m, "week_starts"); err != nil {
		return Identity{}, nil, fmt.Errorf("identity.week_starts: %w", err)
	}
	for i, email := range id.Emails {
		id.Emails[i] = strings.TrimSpace(email)
	}
	id.GitHub = strings.TrimSpace(id.GitHub)
	if tz, ok := canonicalTimezone(id.Timezone); ok {
		id.Timezone = tz
	} else {
		id.Timezone = strings.TrimSpace(id.Timezone)
	}
	if canon, ok := canonicalWeekday(id.WeekStarts); ok {
		id.WeekStarts = canon
	} else if strings.TrimSpace(id.WeekStarts) == "" {
		id.WeekStarts = "monday"
	} else {
		id.WeekStarts = strings.TrimSpace(id.WeekStarts)
	}
	return id, leftoverKeys("identity", m), nil
}

func decodeOutput(m map[string]any) (Output, []string, error) {
	if m == nil {
		return Output{}, nil, nil
	}
	dir, err := takeString(m, "dir")
	if err != nil {
		return Output{}, nil, fmt.Errorf("output.dir: %w", err)
	}
	template, err := takeString(m, "template")
	if err != nil {
		return Output{}, nil, fmt.Errorf("output.template: %w", err)
	}
	dir, err = cleanPath(dir)
	if err != nil {
		return Output{}, nil, fmt.Errorf("output.dir: %w", err)
	}
	template, err = cleanPath(template)
	if err != nil {
		return Output{}, nil, fmt.Errorf("output.template: %w", err)
	}
	return Output{Dir: dir, Template: template}, leftoverKeys("output", m), nil
}

func decodeRedaction(m map[string]any) (Redaction, []string, error) {
	if m == nil {
		return Redaction{}, nil, nil
	}
	patterns, err := takeStrings(m, "extra_patterns")
	if err != nil {
		return Redaction{}, nil, fmt.Errorf("redaction.extra_patterns: %w", err)
	}
	allow, err := takeStrings(m, "allow")
	if err != nil {
		return Redaction{}, nil, fmt.Errorf("redaction.allow: %w", err)
	}
	for i, pattern := range patterns {
		patterns[i] = strings.TrimSpace(pattern)
	}
	for i, item := range allow {
		allow[i] = strings.TrimSpace(item)
	}
	return Redaction{ExtraPatterns: patterns, Allow: allow}, leftoverKeys("redaction", m), nil
}

func decodeSources(raw []map[string]any) ([]Source, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	sources := make([]Source, 0, len(raw))
	for i, table := range raw {
		settings := make(map[string]any, len(table))
		for key, value := range table {
			settings[key] = normalizeValue(value)
		}
		typeValue, err := takeString(settings, "type")
		if err != nil {
			return nil, fmt.Errorf("source[%d].type: %w", i, err)
		}
		if err := expandRoots(i, settings); err != nil {
			return nil, err
		}
		sources = append(sources, Source{
			Type:     strings.TrimSpace(typeValue),
			Settings: settings,
		})
	}
	return sources, nil
}

func expandRoots(index int, settings map[string]any) error {
	value, ok := settings["roots"]
	if !ok {
		return nil
	}
	roots, ok := value.([]string)
	if !ok {
		return nil
	}
	for i, root := range roots {
		expanded, err := cleanPath(root)
		if err != nil {
			return fmt.Errorf("source[%d].roots[%d]: %w", index, i, err)
		}
		roots[i] = expanded
	}
	settings["roots"] = roots
	return nil
}

func takeString(m map[string]any, key string) (string, error) {
	value, ok := m[key]
	if !ok {
		return "", nil
	}
	delete(m, key)
	s, ok := value.(string)
	if !ok {
		return "", errors.New("expected a string")
	}
	return s, nil
}

func takeStrings(m map[string]any, key string) ([]string, error) {
	value, ok := m[key]
	if !ok {
		return nil, nil
	}
	delete(m, key)
	value = normalizeValue(value)
	items, ok := value.([]string)
	if !ok {
		return nil, errors.New("expected an array of strings")
	}
	return items, nil
}

func leftoverKeys(table string, m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, table+"."+key)
	}
	return keys
}

func normalizeValue(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return value
		}
		out[i] = s
	}
	return out
}

func canonicalTimezone(tz string) (string, bool) {
	tz = strings.TrimSpace(tz)
	if tz == "" || strings.EqualFold(tz, "local") {
		return "", true
	}
	if strings.EqualFold(tz, "utc") {
		return "UTC", true
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "", false
	}
	return tz, true
}

func canonicalWeekday(day string) (string, bool) {
	day = strings.ToLower(strings.TrimSpace(day))
	if day == "" {
		return "", false
	}
	if _, ok := weekdays[day]; !ok {
		return "", false
	}
	return day, true
}

var weekdays = map[string]time.Weekday{
	"sunday":    time.Sunday,
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
}

const weekdayList = "sunday, monday, tuesday, wednesday, thursday, friday, or saturday"

// Location returns the configured timezone. An empty timezone is the local zone.
func (id Identity) Location() (*time.Location, error) {
	if id.Timezone == "" || strings.EqualFold(id.Timezone, "local") {
		return time.Local, nil
	}
	if strings.EqualFold(id.Timezone, "utc") {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(id.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %s", quoteUser(id.Timezone))
	}
	return loc, nil
}

// WeekStart returns the configured first day of the week.
// An empty value means Monday.
func (id Identity) WeekStart() (time.Weekday, error) {
	if id.WeekStarts == "" {
		return time.Monday, nil
	}
	day := strings.ToLower(strings.TrimSpace(id.WeekStarts))
	weekday, ok := weekdays[day]
	if !ok {
		return 0, fmt.Errorf("invalid week_starts %s (use %s)", quoteUser(id.WeekStarts), weekdayList)
	}
	return weekday, nil
}
