package report

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed template.md
var builtinTemplate string

const (
	// NotesMarker brackets the manual notes block that overwrite keeps.
	NotesMarker = "<!-- weeklog:notes -->"

	maxTemplateBytes = 1 << 20
	maxReportBytes   = 2 << 20
)

// Markdown renders r with templatePath, or the built-in template when
// templatePath is empty. An empty Notes value becomes a single newline so
// the markers stay on their own lines. Notes already present in the rendered
// text are left in place.
func Markdown(r Report, templatePath string) (string, error) {
	notes := r.Notes
	if notes == "" {
		notes = "\n"
	}
	r.Notes = notes
	body, err := execute(r, templatePath)
	if err != nil {
		return "", err
	}
	return spliceNotes(body, notes), nil
}

// JSON renders r with stable indentation. Notes are omitted.
func JSON(r Report) ([]byte, error) {
	if r.Sections == nil {
		r.Sections = []Section{}
	}
	if r.Other == nil {
		r.Other = []Other{}
	}
	r.Notes = ""
	r.OtherLine = ""
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode report: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

// ExtractNotes returns the text between the first pair of notes markers.
// The second result is false when the file has no pair.
func ExtractNotes(s string) (string, bool) {
	i := strings.Index(s, NotesMarker)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(NotesMarker):]
	j := strings.Index(rest, NotesMarker)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

// WriteMarkdown writes the Markdown report to path. An existing file keeps
// its notes block. The write replaces the file only after the new body is
// complete.
func WriteMarkdown(path, templatePath string, r Report) error {
	notes, err := existingNotes(path)
	if err != nil {
		return err
	}
	r.Notes = notes
	body, err := Markdown(r, templatePath)
	if err != nil {
		return err
	}
	return writeFile(path, []byte(body))
}

// WriteBytes writes body to path, creating parent directories.
func WriteBytes(path string, body []byte) error {
	return writeFile(path, body)
}

func execute(r Report, templatePath string) (string, error) {
	src, err := templateSource(templatePath)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New("week").Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("output template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, r); err != nil {
		return "", fmt.Errorf("output template: %w", err)
	}
	return buf.String(), nil
}

func templateSource(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return builtinTemplate, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("output template not found: %s", path)
		}
		return "", fmt.Errorf("output template: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("output template is a directory: %s", path)
	}
	if info.Size() > maxTemplateBytes {
		return "", fmt.Errorf("output template exceeds %d bytes", maxTemplateBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("output template: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxTemplateBytes+1))
	if err != nil {
		return "", fmt.Errorf("output template: %w", err)
	}
	if int64(len(data)) > maxTemplateBytes {
		return "", fmt.Errorf("output template exceeds %d bytes", maxTemplateBytes)
	}
	return string(data), nil
}

func spliceNotes(rendered, notes string) string {
	i := strings.Index(rendered, NotesMarker)
	if i < 0 {
		if !strings.HasSuffix(rendered, "\n") {
			rendered += "\n"
		}
		return rendered + NotesMarker + notes + NotesMarker + "\n"
	}
	rest := rendered[i+len(NotesMarker):]
	j := strings.Index(rest, NotesMarker)
	if j < 0 {
		suffix := ""
		if !strings.HasSuffix(rendered, "\n") {
			suffix = "\n"
		}
		return rendered[:i+len(NotesMarker)] + notes + NotesMarker + suffix
	}
	return rendered[:i+len(NotesMarker)] + notes + rest[j:]
}

func existingNotes(path string) (string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "\n", nil
	}
	if err != nil {
		return "", fmt.Errorf("read report: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("read report: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("output path is a directory: %s", path)
	}
	if info.Size() > maxReportBytes {
		return "", fmt.Errorf("output file exceeds %d bytes", maxReportBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxReportBytes+1))
	if err != nil {
		return "", fmt.Errorf("read report: %w", err)
	}
	if int64(len(data)) > maxReportBytes {
		return "", fmt.Errorf("output file exceeds %d bytes", maxReportBytes)
	}
	notes, ok := ExtractNotes(string(data))
	if !ok {
		return "\n", nil
	}
	return notes, nil
}

func writeFile(path string, body []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("output path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return fmt.Errorf("output path is a directory: %s", path)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("write report: %w", err)
	}
	f, err := os.CreateTemp(dir, ".weeklog-*.tmp")
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write report: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	ok = true
	return nil
}
