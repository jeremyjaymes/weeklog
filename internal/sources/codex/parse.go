package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxLineBytes = 4 << 20

// partial is the in-memory view of one rollout file.
type partial struct {
	id        string
	cwd       string
	branch    string
	repo      string
	prompt    string
	start     time.Time
	end       time.Time
	hasTime   bool
	metaTime  bool
	files     []string
	tokensIn  float64
	tokensOut float64
}

type envelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type metaDoc struct {
	ID        string   `json:"id"`
	SessionID string   `json:"session_id"`
	Timestamp string   `json:"timestamp"`
	CWD       string   `json:"cwd"`
	Git       *gitInfo `json:"git"`
	Meta      *metaDoc `json:"meta"`
}

type gitInfo struct {
	Branch        string `json:"branch"`
	RepositoryURL string `json:"repository_url"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type tokenUsage struct {
	Input  float64 `json:"input_tokens"`
	Output float64 `json:"output_tokens"`
}

// patchPathRE matches Codex apply_patch file headers. The path is the rest of the line.
var patchPathRE = regexp.MustCompile(`(?m)^\*\*\* (?:(?:Add|Update|Delete) File:|Move to:)[ \t]*(.+)$`)

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
		var env envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			dropped++
			continue
		}
		applyLine(&p, env)
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return p, dropped + 1, true, nil
		}
		return partial{}, 0, false, err
	}
	return p, dropped, false, nil
}

func applyLine(p *partial, env envelope) {
	noteTime(p, env.Timestamp, false)
	switch env.Type {
	case "session_meta":
		applyMeta(p, env.Payload)
	case "turn_context":
		applyTurn(p, env.Payload)
	case "response_item":
		applyResponse(p, env.Payload)
	case "event_msg":
		applyEvent(p, env.Payload)
	}
}

func applyMeta(p *partial, raw json.RawMessage) {
	var doc metaDoc
	if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &doc) != nil {
		return
	}
	nested := doc.Meta
	if p.id == "" {
		p.id = firstText(doc.ID, doc.SessionID)
		if p.id == "" && nested != nil {
			p.id = firstText(nested.ID, nested.SessionID)
		}
	}
	stamp := strings.TrimSpace(doc.Timestamp)
	if stamp == "" && nested != nil {
		stamp = strings.TrimSpace(nested.Timestamp)
	}
	noteTime(p, stamp, true)
	if p.cwd == "" {
		p.cwd = strings.TrimSpace(doc.CWD)
		if p.cwd == "" && nested != nil {
			p.cwd = strings.TrimSpace(nested.CWD)
		}
	}
	if p.branch == "" {
		p.branch = gitBranch(doc.Git)
		if p.branch == "" && nested != nil {
			p.branch = gitBranch(nested.Git)
		}
	}
	if p.repo == "" {
		p.repo = gitRepo(doc.Git)
		if p.repo == "" && nested != nil {
			p.repo = gitRepo(nested.Git)
		}
	}
}

func applyTurn(p *partial, raw json.RawMessage) {
	var body struct {
		CWD string `json:"cwd"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return
	}
	if p.cwd == "" {
		p.cwd = strings.TrimSpace(body.CWD)
	}
}

func applyResponse(p *partial, raw json.RawMessage) {
	var head struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return
	}
	switch head.Type {
	case "message":
		if head.Role != "user" || p.prompt != "" {
			return
		}
		var body struct {
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return
		}
		if text := strings.TrimSpace(textValue(body.Content)); text != "" {
			p.prompt = text
		}
	case "function_call", "custom_tool_call":
		applyTool(p, raw)
	}
}

func applyEvent(p *partial, raw json.RawMessage) {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return
	}
	switch head.Type {
	case "user_message":
		if p.prompt != "" {
			return
		}
		var body struct {
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return
		}
		if text := strings.TrimSpace(textValue(body.Message)); text != "" {
			p.prompt = text
		}
	case "token_count":
		applyTokens(p, raw)
	case "patch_apply_end":
		applyPatchEnd(p, raw)
	}
}

func applyTool(p *partial, raw json.RawMessage) {
	var body struct {
		Arguments json.RawMessage `json:"arguments"`
		Input     json.RawMessage `json:"input"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return
	}
	for _, text := range append(toolTexts(body.Arguments), toolTexts(body.Input)...) {
		p.files = append(p.files, patchPaths(text)...)
	}
}

func applyTokens(p *partial, raw json.RawMessage) {
	var body struct {
		Info *struct {
			Total *tokenUsage `json:"total_token_usage"`
		} `json:"info"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Info == nil || body.Info.Total == nil {
		return
	}
	p.tokensIn = finite(body.Info.Total.Input)
	p.tokensOut = finite(body.Info.Total.Output)
}

func applyPatchEnd(p *partial, raw json.RawMessage) {
	var body struct {
		Changes map[string]json.RawMessage `json:"changes"`
	}
	if json.Unmarshal(raw, &body) != nil || len(body.Changes) == 0 {
		return
	}
	keys := make([]string, 0, len(body.Changes))
	for key := range body.Changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.TrimSpace(key) != "" {
			p.files = append(p.files, key)
		}
	}
}

func toolTexts(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil
		}
		out := []string{text}
		trim := strings.TrimSpace(text)
		if strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[") {
			var v any
			if json.Unmarshal([]byte(trim), &v) == nil {
				collectStrings(v, &out, 0)
			}
		}
		return out
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	collectStrings(v, &out, 0)
	return out
}

func collectStrings(v any, out *[]string, depth int) {
	if v == nil || depth > 8 {
		return
	}
	switch t := v.(type) {
	case string:
		*out = append(*out, t)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for key := range t {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectStrings(t[key], out, depth+1)
		}
	case []any:
		for _, item := range t {
			collectStrings(item, out, depth+1)
		}
	}
}

func patchPaths(text string) []string {
	matches := patchPathRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if path := strings.TrimSpace(match[1]); path != "" {
			out = append(out, path)
		}
	}
	return out
}

func textValue(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return ""
		}
		return text
	}
	if raw[0] == '{' || raw[0] == '[' {
		return contentText(raw)
	}
	return ""
}

func contentText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '{' {
		var block contentBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			return ""
		}
		return block.Text
	}
	if raw[0] != '[' {
		return ""
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, block := range blocks {
		switch block.Type {
		case "text", "input_text", "output_text":
			if strings.TrimSpace(block.Text) == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

func noteTime(p *partial, raw string, meta bool) {
	t, ok := parseTime(raw)
	if !ok {
		return
	}
	if meta {
		p.start = t
		p.metaTime = true
	} else if !p.metaTime && (!p.hasTime || t.Before(p.start)) {
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

func gitBranch(g *gitInfo) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Branch)
}

func gitRepo(g *gitInfo) string {
	if g == nil {
		return ""
	}
	return normalizeRemote(g.RepositoryURL)
}

func firstText(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}
