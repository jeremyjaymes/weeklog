package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	// gitScanDepth matches internal/sources/git maxScanDepth.
	// The Projects directory itself is level 0. A repository stops the walk.
	gitScanDepth = 3

	projectsDirName = "Projects"
	claudeDirName   = ".claude"
	codexDirName    = ".codex"

	outputDirDefault = "~/journal/weeklog"
	probeTimeout     = 15 * time.Second
	maxStatusBytes   = 64 << 10
)

var (
	errNotDirectory = errors.New("not a directory")

	// ghLoggedIn matches current and older `gh auth status` account lines.
	ghLoggedIn = regexp.MustCompile(`Logged in to \S+ (?:account |as )(\S+)`)
)

// InitOptions selects where Init writes the starter config.
type InitOptions struct {
	// Path is the config file to write. A relative path stays relative.
	Path string
	// Force replaces an existing file. Without Force, an existing file is kept.
	Force bool
}

// InitChoices is the config written by init.
// Empty OutputDir, OutputMode, and WeekStarts use the starter defaults.
// An empty GitHubToken with GitHubSource writes token = "cmd:gh auth token".
// A non-empty token is stored as a literal. Linear and Cursor Cloud require one.
type InitChoices struct {
	Email        string
	GitHub       string
	Timezone     string
	WeekStarts   string
	OutputDir    string
	OutputMode   string
	Git          bool
	Claude       bool
	Codex        bool
	GitHubSource bool
	GitHubToken  string
	Linear       bool
	LinearToken  string
	CursorCloud  bool
	CursorToken  string
}

// SourceStatus is one source init looked for.
// Detail is safe to print and does not include secret values.
type SourceStatus struct {
	Type    string
	Enabled bool
	Detail  string
}

// InitResult is the starter config init built.
// Body is the TOML that was written, or that would be written when a file
// already exists. Literal token values are replaced with "[redacted]".
// Secret references and empty placeholders are kept.
type InitResult struct {
	Path    string
	Wrote   bool
	Existed bool
	Body    string
	Sources []SourceStatus
}

// Init detects local sources and writes a starter config to opts.Path.
// An existing file is left in place unless Force is set. The result still
// describes what would have been written.
//
// Claude Code is enabled when $CLAUDE_CONFIG_DIR is a directory, or when
// that variable is unset and ~/.claude is a directory. Codex uses
// $CODEX_HOME the same way, otherwise ~/.codex. GitHub is enabled when gh
// is on PATH, gh auth status succeeds, and gh auth token prints a token.
// The written file is mode 0600. GitHub's token is cmd:gh auth token.
// A commented Linear block uses token = "" and tells the user to paste a key.
// A commented cursor-cloud block does the same for one Cursor API key.
// Git is enabled when ~/Projects contains a repository within gitScanDepth.
func Init(ctx context.Context, opts InitOptions) (InitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return InitResult{}, err
	}
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return InitResult{}, errors.New("config path is empty")
	}
	path = filepath.Clean(path)

	choices, sources, err := Detect(ctx)
	result := InitResult{Path: path, Sources: sources}
	if err != nil {
		return result, err
	}
	return writeInit(path, choices, sources, opts.Force)
}

// Detect reports the sources and identity the non-interactive starter uses.
// It does not write a file. Cursor Cloud and Linear are left unset; the
// interactive form offers them.
func Detect(ctx context.Context) (InitChoices, []SourceStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return InitChoices{}, nil, err
	}
	home, err := homeDir()
	if err != nil {
		return InitChoices{}, nil, err
	}

	var sources []SourceStatus
	gitStatus, email, err := detectGit(ctx, home)
	sources = append(sources, gitStatus)
	if err != nil {
		return InitChoices{}, sources, err
	}
	claudeStatus, err := detectEnvDir("claude-code", "CLAUDE_CONFIG_DIR", claudeDirName, home)
	sources = append(sources, claudeStatus)
	if err != nil {
		return InitChoices{}, sources, err
	}
	codexStatus, err := detectEnvDir("codex", "CODEX_HOME", codexDirName, home)
	sources = append(sources, codexStatus)
	if err != nil {
		return InitChoices{}, sources, err
	}
	githubStatus, login, err := detectGitHub(ctx)
	sources = append(sources, githubStatus)
	if err != nil {
		return InitChoices{}, sources, err
	}

	return InitChoices{
		Email:        email,
		GitHub:       login,
		Timezone:     localTimezone(),
		WeekStarts:   "monday",
		OutputDir:    outputDirDefault,
		OutputMode:   OutputModeBoth,
		Git:          gitStatus.Enabled,
		Claude:       claudeStatus.Enabled,
		Codex:        codexStatus.Enabled,
		GitHubSource: githubStatus.Enabled,
	}, sources, nil
}

// WriteInitConfig renders choices and writes one config file.
// An existing file is left unchanged unless replace is true.
// The result body is safe to print: literal tokens are redacted.
// The file is mode 0600. No other file, including a .env, is written.
func WriteInitConfig(path string, choices InitChoices, replace bool) (InitResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return InitResult{}, errors.New("config path is empty")
	}
	return writeInit(filepath.Clean(path), choices, nil, replace)
}

func writeInit(path string, choices InitChoices, sources []SourceStatus, replace bool) (InitResult, error) {
	result := InitResult{Path: path}
	choices = normalizeInitChoices(choices)
	if err := validateInitChoices(choices); err != nil {
		return result, err
	}
	if sources == nil {
		sources = choiceStatuses(choices)
	}
	result.Sources = sources

	body, err := renderStarter(choices)
	if err != nil {
		return result, err
	}
	result.Body = redactTokenLiterals(body, literalTokens(choices))

	existed, err := existingConfig(path)
	if err != nil {
		return result, err
	}
	if existed && !replace {
		result.Existed = true
		return result, nil
	}
	if err := writeConfigFile(path, body); err != nil {
		return result, err
	}
	result.Wrote = true
	result.Existed = existed
	return result, nil
}

func normalizeInitChoices(c InitChoices) InitChoices {
	c.Email = strings.TrimSpace(c.Email)
	c.GitHub = strings.TrimSpace(c.GitHub)
	c.Timezone = strings.TrimSpace(c.Timezone)
	c.WeekStarts = strings.TrimSpace(c.WeekStarts)
	if c.WeekStarts == "" {
		c.WeekStarts = "monday"
	}
	c.OutputDir = strings.TrimSpace(c.OutputDir)
	if c.OutputDir == "" {
		c.OutputDir = outputDirDefault
	}
	c.OutputMode = strings.ToLower(strings.TrimSpace(c.OutputMode))
	if c.OutputMode == "" {
		c.OutputMode = OutputModeBoth
	}
	c.GitHubToken = strings.TrimSpace(c.GitHubToken)
	c.LinearToken = strings.TrimSpace(c.LinearToken)
	c.CursorToken = strings.TrimSpace(c.CursorToken)
	return c
}

func validateInitChoices(c InitChoices) error {
	if err := ValidateOutputDir(c.OutputDir); err != nil {
		return err
	}
	if err := ValidateOutputMode(c.OutputMode); err != nil {
		return err
	}
	if err := ValidateLiteralToken(c.GitHubToken, false); err != nil {
		return fmt.Errorf("github token: %w", err)
	}
	if c.Linear {
		if err := ValidateLiteralToken(c.LinearToken, true); err != nil {
			return fmt.Errorf("linear token: %w", err)
		}
	}
	if c.CursorCloud {
		if err := ValidateLiteralToken(c.CursorToken, true); err != nil {
			return fmt.Errorf("cursor-cloud token: %w", err)
		}
	}
	return nil
}

// ValidateOutputDir reports whether dir can be written as output.dir.
func ValidateOutputDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("output dir is empty")
	}
	if containsControl(dir) {
		return errors.New("output dir contains a control character")
	}
	return nil
}

// ValidateOutputMode reports whether mode is both, file, or terminal.
// An empty mode is accepted and means both.
func ValidateOutputMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", OutputModeBoth, OutputModeFile, OutputModeTerminal:
		return nil
	default:
		return fmt.Errorf("invalid output mode %s", quoteUser(mode))
	}
}

// ValidateLiteralToken reports whether token can be stored as a literal.
// An empty token is allowed when required is false.
// env:, cmd:, and file: prefixes are rejected so the value is the token itself.
func ValidateLiteralToken(token string, required bool) error {
	token = strings.TrimSpace(token)
	if token == "" {
		if required {
			return errors.New("token is empty")
		}
		return nil
	}
	if containsControl(token) {
		return errors.New("token contains a control character")
	}
	ref, err := parseSecretRef(token)
	if err != nil || ref.Kind != refLiteral {
		return errors.New("paste the token itself")
	}
	return nil
}

func choiceStatuses(c InitChoices) []SourceStatus {
	return []SourceStatus{
		flagStatus("git", c.Git),
		flagStatus("claude-code", c.Claude),
		flagStatus("codex", c.Codex),
		flagStatus("github", c.GitHubSource),
		flagStatus("linear", c.Linear),
		flagStatus("cursor-cloud", c.CursorCloud),
	}
}

func flagStatus(typ string, enabled bool) SourceStatus {
	detail := "not selected"
	if enabled {
		detail = "selected"
	}
	return SourceStatus{Type: typ, Enabled: enabled, Detail: detail}
}

func literalTokens(c InitChoices) []string {
	var out []string
	if c.GitHubSource && isLiteralToken(c.GitHubToken) {
		out = append(out, c.GitHubToken)
	}
	if c.Linear && isLiteralToken(c.LinearToken) {
		out = append(out, c.LinearToken)
	}
	if c.CursorCloud && isLiteralToken(c.CursorToken) {
		out = append(out, c.CursorToken)
	}
	return out
}

func isLiteralToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	ref, err := parseSecretRef(token)
	return err == nil && ref.Kind == refLiteral
}

func redactTokenLiterals(body string, tokens []string) string {
	sorted := append([]string(nil), tokens...)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i]) > len(sorted[j])
	})
	for _, token := range sorted {
		quoted, err := tomlBasicString(token)
		if err != nil {
			continue
		}
		body = strings.ReplaceAll(body, quoted, `"[redacted]"`)
	}
	return body
}

// Report returns the human-readable init summary.
// It lists each source and either the path written or the TOML that was
// kept off disk. Secret values are never included.
func (r InitResult) Report() string {
	if len(r.Sources) == 0 && r.Body == "" {
		return ""
	}
	var b strings.Builder
	for _, src := range r.Sources {
		state := "skipped"
		if src.Enabled {
			state = "enabled"
		}
		fmt.Fprintf(&b, "%s: %s (%s)\n", oneLine(src.Type), state, oneLine(src.Detail))
	}
	switch {
	case r.Wrote:
		fmt.Fprintf(&b, "wrote: %s\n\n%s", oneLine(r.Path), r.Body)
	case r.Existed:
		fmt.Fprintf(&b, "config already exists: %s\nnot overwritten; pass --force to replace it\nwould write:\n\n%s", oneLine(r.Path), r.Body)
	}
	return b.String()
}

func renderStarter(s InitChoices) (string, error) {
	var b strings.Builder
	b.WriteString("# Written by weeklog init.\n\n[identity]\n")
	if s.Email != "" {
		if err := writeStrings(&b, "emails", []string{s.Email}); err != nil {
			return "", err
		}
	}
	if s.GitHub != "" {
		if err := writeString(&b, "github", s.GitHub); err != nil {
			return "", err
		}
	}
	if s.Timezone != "" {
		if err := writeString(&b, "timezone", s.Timezone); err != nil {
			return "", err
		}
	}
	if err := writeString(&b, "week_starts", s.WeekStarts); err != nil {
		return "", err
	}
	b.WriteString("\n[output]\n")
	if err := writeString(&b, "dir", s.OutputDir); err != nil {
		return "", err
	}
	b.WriteString("# both writes the file and prints the recap. file writes only. terminal prints only.\n")
	if err := writeString(&b, "mode", s.OutputMode); err != nil {
		return "", err
	}
	if err := writeString(&b, "template", ""); err != nil {
		return "", err
	}
	if s.Git {
		b.WriteString("\n[[source]]\n")
		if err := writeString(&b, "type", "git"); err != nil {
			return "", err
		}
		if err := writeStrings(&b, "roots", []string{"~/Projects"}); err != nil {
			return "", err
		}
		if s.Email != "" {
			if err := writeStrings(&b, "authors", []string{s.Email}); err != nil {
				return "", err
			}
		}
	}
	if s.Claude {
		b.WriteString("\n[[source]]\n")
		if err := writeString(&b, "type", "claude-code"); err != nil {
			return "", err
		}
	}
	if s.Codex {
		b.WriteString("\n[[source]]\n")
		if err := writeString(&b, "type", "codex"); err != nil {
			return "", err
		}
	}
	if s.GitHubSource {
		b.WriteString("\n[[source]]\n")
		if err := writeString(&b, "type", "github"); err != nil {
			return "", err
		}
		token := "cmd:gh auth token"
		if s.GitHubToken != "" {
			token = s.GitHubToken
		}
		if err := writeString(&b, "token", token); err != nil {
			return "", err
		}
	}
	if s.Linear {
		b.WriteString("\n[[source]]\n")
		if err := writeString(&b, "type", "linear"); err != nil {
			return "", err
		}
		if err := writeString(&b, "token", s.LinearToken); err != nil {
			return "", err
		}
	} else {
		writeLinearPlaceholder(&b)
	}
	if s.CursorCloud {
		b.WriteString("\n[[source]]\n")
		if err := writeString(&b, "type", "cursor-cloud"); err != nil {
			return "", err
		}
		if err := writeString(&b, "token", s.CursorToken); err != nil {
			return "", err
		}
	} else {
		writeCursorCloudPlaceholder(&b)
	}
	return b.String(), nil
}

// writeLinearPlaceholder appends a commented Linear source. The empty token
// is the placeholder a user replaces. GitHub keeps cmd:gh auth token instead.
func writeLinearPlaceholder(b *strings.Builder) {
	b.WriteString("\n# Linear is not detected. Paste your key into token.\n")
	b.WriteString("# [[source]]\n")
	b.WriteString("# type = \"linear\"\n")
	b.WriteString("# token = \"\"\n")
}

// writeCursorCloudPlaceholder appends a commented Cloud Agents source.
// The empty token is the placeholder a user replaces with one literal key.
func writeCursorCloudPlaceholder(b *strings.Builder) {
	b.WriteString("\n# Cursor cloud agents are not detected. Paste your API key into token.\n")
	b.WriteString("# [[source]]\n")
	b.WriteString("# type = \"cursor-cloud\"\n")
	b.WriteString("# token = \"\"\n")
}

func writeString(b *strings.Builder, key, value string) error {
	quoted, err := tomlBasicString(value)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	fmt.Fprintf(b, "%s = %s\n", key, quoted)
	return nil
}

func writeStrings(b *strings.Builder, key string, values []string) error {
	parts := make([]string, len(values))
	for i, value := range values {
		quoted, err := tomlBasicString(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		parts[i] = quoted
	}
	fmt.Fprintf(b, "%s = [%s]\n", key, strings.Join(parts, ", "))
	return nil
}

func tomlBasicString(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cc, r) {
			return "", errors.New("value contains a control character")
		}
		if r == '\\' || r == '"' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String(), nil
}

func detectEnvDir(sourceType, envKey, fallbackName, home string) (SourceStatus, error) {
	status := SourceStatus{Type: sourceType}
	label := "$" + envKey
	path := ""
	if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
		expanded, err := cleanPath(raw)
		if err != nil {
			return status, fmt.Errorf("detect %s: %w", sourceType, err)
		}
		path = expanded
	} else {
		path = filepath.Join(home, fallbackName)
		label = "~/" + fallbackName
	}
	err := dirKind(path)
	switch {
	case err == nil:
		status.Enabled = true
		status.Detail = label
	case errors.Is(err, os.ErrNotExist):
		status.Detail = label + " not found"
	case errors.Is(err, errNotDirectory):
		status.Detail = label + " is not a directory"
	default:
		return status, fmt.Errorf("detect %s: %w", sourceType, err)
	}
	return status, nil
}

func detectGit(ctx context.Context, home string) (SourceStatus, string, error) {
	status := SourceStatus{Type: "git"}
	email, err := gitEmail(ctx)
	if err != nil {
		return status, "", err
	}
	n, err := countGitRepos(filepath.Join(home, projectsDirName))
	switch {
	case err == nil && n > 0:
		status.Enabled = true
		status.Detail = repoDetail(n)
	case errors.Is(err, os.ErrNotExist):
		status.Detail = "~/Projects not found"
	case errors.Is(err, errNotDirectory):
		status.Detail = "~/Projects is not a directory"
	case err != nil:
		return status, "", fmt.Errorf("detect git: %w", err)
	default:
		status.Detail = "no repositories under ~/Projects"
	}
	return status, email, nil
}

func repoDetail(n int) string {
	if n == 1 {
		return "1 repository under ~/Projects"
	}
	return fmt.Sprintf("%d repositories under ~/Projects", n)
}

func detectGitHub(ctx context.Context) (SourceStatus, string, error) {
	status := SourceStatus{Type: "github"}
	path, err := exec.LookPath("gh")
	if err != nil {
		status.Detail = "gh not on PATH"
		return status, "", nil
	}
	text, err := runCommandText(ctx, true, path, "auth", "status")
	if err != nil {
		if hard, ok := hardProbeErr("gh auth status", err); ok {
			return status, "", fmt.Errorf("detect github: %w", hard)
		}
		status.Detail = "gh is not authenticated"
		return status, "", nil
	}
	login := strings.Clone(parseGHLogin(text))
	if !validGitHubLogin(login) {
		login = ""
	}
	if err := ghTokenPrints(ctx, path); err != nil {
		if hard, ok := hardProbeErr("gh auth token", err); ok {
			return status, "", fmt.Errorf("detect github: %w", hard)
		}
		status.Detail = "gh is not authenticated"
		return status, "", nil
	}
	status.Enabled = true
	status.Detail = "gh auth"
	if login != "" {
		status.Detail = "gh auth, login " + login
	}
	return status, login, nil
}

func gitEmail(ctx context.Context) (string, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return "", nil
	}
	text, err := runCommandText(ctx, false, path, "config", "--global", "user.email")
	if err != nil {
		if hard, ok := hardProbeErr("git config", err); ok {
			return "", fmt.Errorf("detect git: %w", hard)
		}
		return "", nil
	}
	return usableText(text), nil
}

// ghTokenPrints runs `gh auth token` and reports whether it printed a
// non-empty token. The token bytes are discarded and are not returned.
func ghTokenPrints(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, "auth", "token")
	cmd.Env = mergeEnv("GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	saw := &nonSpaceWriter{}
	cmd.Stdout = saw
	err := cmd.Run()
	if err != nil {
		return probeFailure(ctx, probeCtx, err)
	}
	if !saw.saw {
		return errors.New("empty token")
	}
	return nil
}

// nonSpaceWriter reports whether any non-space byte was written.
// It does not retain the bytes.
type nonSpaceWriter struct {
	saw bool
}

func (w *nonSpaceWriter) Write(p []byte) (int, error) {
	if !w.saw {
		for _, c := range p {
			if c > ' ' && c != 0x7f {
				w.saw = true
				break
			}
		}
	}
	return len(p), nil
}

func runCommandText(ctx context.Context, combineStderr bool, path string, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, args...)
	cmd.Env = mergeEnv("GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	cmd.WaitDelay = time.Second
	var buf bytes.Buffer
	cmd.Stdout = &cappedWriter{buf: &buf, max: maxStatusBytes}
	cmd.Stderr = io.Discard
	if combineStderr {
		cmd.Stderr = cmd.Stdout
	}
	err := cmd.Run()
	if err != nil {
		return "", probeFailure(ctx, probeCtx, err)
	}
	text := buf.String()
	buf.Reset()
	return text, nil
}

func probeFailure(ctx, probeCtx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if probeCtx.Err() != nil {
		return probeCtx.Err()
	}
	return err
}

func hardProbeErr(what string, err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", what, err), true
	}
	return nil, false
}

func parseGHLogin(text string) string {
	match := ghLoggedIn.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimRight(match[1], ").,")
}

func validGitHubLogin(s string) bool {
	if len(s) < 1 || len(s) > 39 {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	prevHyphen := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			prevHyphen = false
		case r == '-' && !prevHyphen:
			prevHyphen = true
		default:
			return false
		}
	}
	return true
}

func usableText(s string) string {
	s = firstLine(s)
	if s == "" || len(s) > 200 || strings.ContainsAny(s, " \t\r\n") || containsControl(s) {
		return ""
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func localTimezone() string {
	if tz := validZone(os.Getenv("TZ")); tz != "" {
		return tz
	}
	if tz := validZone(time.Local.String()); tz != "" {
		return tz
	}
	return zoneFromLocaltime()
}

func validZone(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "local") {
		return ""
	}
	if strings.EqualFold(name, "utc") {
		return "UTC"
	}
	if _, err := time.LoadLocation(name); err != nil {
		return ""
	}
	return name
}

func zoneFromLocaltime() string {
	const marker = "zoneinfo/"
	path, err := filepath.EvalSymlinks("/etc/localtime")
	if err != nil {
		return ""
	}
	i := strings.LastIndex(path, marker)
	if i < 0 {
		return ""
	}
	return validZone(path[i+len(marker):])
}

func dirKind(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errNotDirectory
	}
	return nil
}

func countGitRepos(root string) (int, error) {
	info, err := os.Stat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, errNotDirectory
	}
	var n int
	if err := walkGit(root, 0, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func walkGit(dir string, depth int, n *int) error {
	if isGitRepo(dir) {
		*n++
		return nil
	}
	if depth >= gitScanDepth {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if depth == 0 {
			return err
		}
		return nil
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == ".git" {
			continue
		}
		if err := walkGit(filepath.Join(dir, name), depth+1, n); err != nil {
			return err
		}
	}
	return nil
}

func isGitRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("find home directory: home is empty")
	}
	return home, nil
}

// ConfigExists reports whether path is an existing config file.
// A directory is an error. A missing path reports false.
func ConfigExists(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, errors.New("config path is empty")
	}
	return existingConfig(filepath.Clean(path))
}

func existingConfig(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read config path: %w", err)
	}
	if info.IsDir() {
		return false, fmt.Errorf("config path is a directory: %s", path)
	}
	return true, nil
}

func writeConfigFile(path, body string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".weeklog-config-*.tmp")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := io.WriteString(f, body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// 0600 regardless of umask. A literal token in this file must not be
	// group- or world-readable.
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	ok = true
	return nil
}

func mergeEnv(overrides ...string) []string {
	skip := make(map[string]struct{}, len(overrides))
	for _, ov := range overrides {
		key, _, _ := strings.Cut(ov, "=")
		if key != "" {
			skip[key] = struct{}{}
		}
	}
	base := os.Environ()
	out := make([]string, 0, len(base)+len(overrides))
	for _, env := range base {
		key, _, _ := strings.Cut(env, "=")
		if _, drop := skip[key]; drop {
			continue
		}
		out = append(out, env)
	}
	return append(out, overrides...)
}

type cappedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.buf.Len() < w.max {
		remain := w.max - w.buf.Len()
		chunk := p
		if len(chunk) > remain {
			chunk = chunk[:remain]
		}
		_, _ = w.buf.Write(chunk)
	}
	return len(p), nil
}
