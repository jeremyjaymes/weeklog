package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"time"
)

const maxLineBytes = 4 << 20

// partial is the in-memory view of one transcript file before it is folded.
type partial struct {
	cwd        string
	branch     string
	prompt     string
	sidePrompt string
	metaPrompt string
	start      time.Time
	end        time.Time
	hasTime    bool
	files      []string
	tokensIn   float64
	tokensOut  float64
}

type record struct {
	Type        string   `json:"type"`
	Timestamp   string   `json:"timestamp"`
	CWD         string   `json:"cwd"`
	GitBranch   string   `json:"gitBranch"`
	IsSidechain bool     `json:"isSidechain"`
	IsMeta      bool     `json:"isMeta"`
	Message     *message `json:"message"`
	Usage       *usage   `json:"usage"`
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type usage struct {
	Input         float64 `json:"input_tokens"`
	Output        float64 `json:"output_tokens"`
	CacheCreation float64 `json:"cache_creation_input_tokens"`
	CacheRead     float64 `json:"cache_read_input_tokens"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type toolInput struct {
	FilePath      string `json:"file_path"`
	FilePathCamel string `json:"filePath"`
	NotebookPath  string `json:"notebook_path"`
	NotebookCamel string `json:"notebookPath"`
}

func parseGroup(g group) (partial, string, int, bool, error) {
	var (
		combined   partial
		dropped    int
		incomplete bool
		body       string
	)
	if g.parent != "" {
		part, n, inc, err := parseFile(g.parent)
		if err != nil {
			return partial{}, "", 0, false, err
		}
		combined = part
		dropped += n
		incomplete = inc
		body = g.parent
	}
	for _, sub := range g.subs {
		part, n, inc, err := parseFile(sub)
		if err != nil {
			return partial{}, "", 0, false, err
		}
		combined.mergeSub(part)
		dropped += n
		if inc {
			incomplete = true
		}
		if body == "" {
			body = sub
		}
	}
	return combined, body, dropped, incomplete, nil
}

func parseFile(path string) (partial, int, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return partial{}, 0, false, err
	}
	defer func() { _ = f.Close() }()

	var (
		p       partial
		dropped int
		first   = true
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			dropped++
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			dropped++
			continue
		}
		applyRecord(&p, rec)
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return p, dropped + 1, true, nil
		}
		return partial{}, 0, false, err
	}
	return p, dropped, false, nil
}

func applyRecord(p *partial, rec record) {
	if p.cwd == "" {
		p.cwd = strings.TrimSpace(rec.CWD)
	}
	if p.branch == "" {
		p.branch = strings.TrimSpace(rec.GitBranch)
	}
	if recordKind(rec) == "user" || recordKind(rec) == "assistant" {
		addTime(p, rec.Timestamp)
	}
	if rec.Message == nil {
		if recordKind(rec) == "assistant" {
			addUsage(p, rec.Usage)
		}
		return
	}
	text, files, toolOnly := contentParts(rec.Message.Content)
	p.files = append(p.files, files...)
	if recordKind(rec) == "assistant" {
		usage := rec.Message.Usage
		if usage == nil {
			usage = rec.Usage
		}
		addUsage(p, usage)
	}
	text = strings.TrimSpace(text)
	if recordKind(rec) != "user" || text == "" || toolOnly {
		return
	}
	switch {
	case rec.IsMeta:
		if p.metaPrompt == "" {
			p.metaPrompt = text
		}
	case rec.IsSidechain:
		if p.sidePrompt == "" {
			p.sidePrompt = text
		}
	default:
		if p.prompt == "" {
			p.prompt = text
		}
	}
}

func recordKind(rec record) string {
	if rec.Type != "" {
		return rec.Type
	}
	if rec.Message != nil && rec.Message.Role != "" {
		return rec.Message.Role
	}
	return ""
}

func addTime(p *partial, raw string) {
	t, ok := parseTime(raw)
	if !ok {
		return
	}
	if !p.hasTime || t.Before(p.start) {
		p.start = t
	}
	if !p.hasTime || t.After(p.end) {
		p.end = t
	}
	p.hasTime = true
}

func parseTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func contentParts(raw json.RawMessage) (string, []string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil, false
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return "", nil, false
		}
		return text, nil, false
	}
	if raw[0] != '[' {
		return "", nil, false
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil, false
	}
	var (
		b             strings.Builder
		files         []string
		sawText       bool
		sawToolResult bool
	)
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if strings.TrimSpace(block.Text) == "" {
				continue
			}
			sawText = true
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(block.Text)
		case "tool_use":
			if path := toolPath(block); path != "" {
				files = append(files, path)
			}
		case "tool_result":
			sawToolResult = true
		}
	}
	return b.String(), files, sawToolResult && !sawText
}

func toolPath(block contentBlock) string {
	if !isFileTool(block.Name) || len(bytes.TrimSpace(block.Input)) == 0 {
		return ""
	}
	var in toolInput
	if err := json.Unmarshal(block.Input, &in); err != nil {
		return ""
	}
	if block.Name == "NotebookEdit" {
		return firstText(in.NotebookPath, in.NotebookCamel)
	}
	return firstText(in.FilePath, in.FilePathCamel)
}

func isFileTool(name string) bool {
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	default:
		return false
	}
}

func firstText(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func addUsage(p *partial, u *usage) {
	if u == nil {
		return
	}
	p.tokensIn += finite(u.Input) + finite(u.CacheCreation) + finite(u.CacheRead)
	p.tokensOut += finite(u.Output)
}

func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

func (p *partial) merge(other partial) {
	if p.cwd == "" {
		p.cwd = other.cwd
	}
	if p.branch == "" {
		p.branch = other.branch
	}
	if p.prompt == "" {
		p.prompt = other.prompt
	}
	if p.sidePrompt == "" {
		p.sidePrompt = other.sidePrompt
	}
	if p.metaPrompt == "" {
		p.metaPrompt = other.metaPrompt
	}
	if other.hasTime {
		if !p.hasTime || other.start.Before(p.start) {
			p.start = other.start
		}
		if !p.hasTime || other.end.After(p.end) {
			p.end = other.end
		}
		p.hasTime = true
	}
	p.files = append(p.files, other.files...)
	p.tokensIn += other.tokensIn
	p.tokensOut += other.tokensOut
}

// mergeSub folds a sub-agent transcript into the parent. The sub-agent prompt
// does not replace the parent's first prompt.
func (p *partial) mergeSub(other partial) {
	if p.sidePrompt == "" {
		switch {
		case other.sidePrompt != "":
			p.sidePrompt = other.sidePrompt
		case other.prompt != "":
			p.sidePrompt = other.prompt
		}
	}
	other.prompt = ""
	other.sidePrompt = ""
	p.merge(other)
}
