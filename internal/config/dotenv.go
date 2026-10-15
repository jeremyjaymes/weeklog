package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	envFileName = ".env"
	maxEnvBytes = 1 << 20
)

// dotEnvFile is a .env file loaded from the config directory.
// values holds trimmed KEY=VALUE entries. issues are safe to print.
type dotEnvFile struct {
	values map[string]string
	issues []Issue
}

// secretValues returns non-empty values in key order.
// Callers must not log them.
func (d dotEnvFile) secretValues() []string {
	if len(d.values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(d.values))
	for key, value := range d.values {
		if value == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, key := range keys {
		out[i] = d.values[key]
	}
	return out
}

func envFilePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), envFileName)
}

// loadDotEnv reads dir/.env. A missing file is not an error.
// A group- or world-readable file is a warning and is still loaded.
// Real environment variables are not modified.
func loadDotEnv(dir string) dotEnvFile {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	path := filepath.Join(dir, envFileName)
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return dotEnvFile{}
		}
		return dotEnvFile{issues: []Issue{errIssue(".env", "cannot read file")}}
	}
	if info.IsDir() {
		return dotEnvFile{issues: []Issue{errIssue(".env", "path is a directory")}}
	}

	var issues []Issue
	if envFileReadableByOthers(info) {
		issues = append(issues, warn(".env", "file is group- or world-readable"))
	}
	if !info.Mode().IsRegular() {
		issues = append(issues, errIssue(".env", "cannot read file"))
		return dotEnvFile{issues: issues}
	}
	if info.Size() > maxEnvBytes {
		issues = append(issues, errIssue(".env", fmt.Sprintf("file exceeds %d bytes", maxEnvBytes)))
		return dotEnvFile{issues: issues}
	}

	f, err := os.Open(path)
	if err != nil {
		issues = append(issues, errIssue(".env", "cannot read file"))
		return dotEnvFile{issues: issues}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEnvBytes+1))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		issues = append(issues, errIssue(".env", "cannot read file"))
		return dotEnvFile{issues: issues}
	}
	if int64(len(data)) > maxEnvBytes {
		issues = append(issues, errIssue(".env", fmt.Sprintf("file exceeds %d bytes", maxEnvBytes)))
		return dotEnvFile{issues: issues}
	}

	values, err := parseDotEnv(string(data))
	if err != nil {
		issues = append(issues, errIssue(".env", err.Error()))
		return dotEnvFile{issues: issues}
	}
	return dotEnvFile{values: values, issues: issues}
}

// envFileReadableByOthers reports whether info is group- or world-readable.
// Windows file modes do not record those bits.
func envFileReadableByOthers(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return info.Mode().Perm()&0o044 != 0
}

func parseDotEnv(data string) (map[string]string, error) {
	if strings.Contains(data, "\x00") {
		return nil, errors.New("invalid .env")
	}
	data = strings.TrimPrefix(data, "\uFEFF")
	values := make(map[string]string)
	for i, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		if err := parseDotEnvLine(line, values); err != nil {
			return nil, fmt.Errorf("invalid .env entry on line %d", i+1)
		}
	}
	return values, nil
}

func parseDotEnvLine(line string, values map[string]string) error {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil
	}
	body := trimmed
	if rest, ok := strings.CutPrefix(body, "export"); ok {
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
			body = strings.TrimSpace(rest)
		}
	}
	key, raw, ok := strings.Cut(body, "=")
	if !ok {
		return errors.New("invalid .env entry")
	}
	key = strings.TrimSpace(key)
	if !validEnvName(key) {
		return errors.New("invalid .env entry")
	}
	value, err := parseEnvValue(raw)
	if err != nil {
		return err
	}
	values[key] = strings.TrimSpace(value)
	return nil
}

func parseEnvValue(raw string) (string, error) {
	trimmed := strings.TrimLeft(raw, " \t")
	if trimmed == "" {
		return "", nil
	}
	switch trimmed[0] {
	case '"':
		return unquoteDouble(trimmed[1:])
	case '\'':
		return unquoteSingle(trimmed[1:])
	default:
		return unquotedValue(raw), nil
	}
}

func unquotedValue(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && i > 0 && (raw[i-1] == ' ' || raw[i-1] == '\t') {
			break
		}
		b.WriteByte(raw[i])
	}
	return strings.TrimSpace(b.String())
}

func unquoteDouble(raw string) (string, error) {
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\\' {
			if i+1 >= len(raw) {
				return "", errors.New("invalid .env entry")
			}
			i++
			switch raw[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '\\', '"', '\'':
				b.WriteByte(raw[i])
			default:
				return "", errors.New("invalid .env entry")
			}
			continue
		}
		if c == '"' {
			return finishQuoted(b.String(), raw[i+1:])
		}
		if c == '\r' {
			return "", errors.New("invalid .env entry")
		}
		b.WriteByte(c)
	}
	return "", errors.New("invalid .env entry")
}

func unquoteSingle(raw string) (string, error) {
	i := strings.IndexByte(raw, '\'')
	if i < 0 {
		return "", errors.New("invalid .env entry")
	}
	return finishQuoted(raw[:i], raw[i+1:])
}

func finishQuoted(value, rest string) (string, error) {
	rest = strings.TrimSpace(rest)
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", errors.New("invalid .env entry")
	}
	return value, nil
}
