package cursorcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

const (
	defaultEndpoint   = "https://api.cursor.com"
	pageSize          = 100
	maxPages          = 20
	maxResponseBytes  = 8 << 20
	requestsPerMinute = 20
	userAgent         = "weeklog"
)

// defaultHTTP does not follow redirects, so the token stays on the API host.
var defaultHTTP = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var retryBackoffs = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// pace spaces requests at 20 per minute across the collect.
type pace struct {
	limit  int
	window time.Duration
	stamps []time.Time
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
}

func (s *Source) endpointURL() string {
	if s != nil && s.endpoint != "" {
		return s.endpoint
	}
	return defaultEndpoint
}

// withLoopback returns s, or a copy whose API origin is a loopback endpoint
// from config. An endpoint already set on s wins, so unit tests that point
// the field at a fixture server keep that URL. The copy leaves the caller
// unchanged.
func (s *Source) withLoopback(config map[string]any) *Source {
	if s == nil || s.endpoint != "" {
		return s
	}
	endpoint, ok := source.LoopbackEndpoint(config)
	if !ok {
		return s
	}
	cp := *s
	cp.endpoint = endpoint
	return &cp
}

func (s *Source) httpClient() *http.Client {
	if s != nil && s.client != nil {
		return s.client
	}
	return defaultHTTP
}

func (s *Source) pages() int {
	if s != nil && s.pageLimit > 0 {
		return s.pageLimit
	}
	return maxPages
}

func (s *Source) maxBody() int {
	if s != nil && s.bodyLimit > 0 {
		return s.bodyLimit
	}
	return maxResponseBytes
}

func (s *Source) clock() (func() time.Time, func(context.Context, time.Duration) error) {
	now := time.Now
	sleep := sleepContext
	if s != nil && s.now != nil {
		now = s.now
	}
	if s != nil && s.sleep != nil {
		sleep = s.sleep
	}
	return now, sleep
}

func (s *Source) newPace() *pace {
	now, sleep := s.clock()
	limit := requestsPerMinute
	window := time.Minute
	if s != nil && s.paceLimit > 0 {
		limit = s.paceLimit
	}
	if s != nil && s.paceWindow > 0 {
		window = s.paceWindow
	}
	return &pace{limit: limit, window: window, now: now, sleep: sleep}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *pace) wait(ctx context.Context) error {
	if p == nil || p.limit <= 0 {
		return nil
	}
	for spins := 0; spins < p.limit+2; spins++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := p.now()
		p.prune(now)
		if len(p.stamps) < p.limit {
			p.stamps = append(p.stamps, now)
			return nil
		}
		wait := p.stamps[0].Add(p.window).Sub(now)
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		if !deadlineAllows(ctx, now, wait) {
			return failedErr(rateLimited)
		}
		before := now
		if err := p.sleep(ctx, wait); err != nil {
			return err
		}
		if !p.now().After(before) {
			return failedErr(rateLimited)
		}
	}
	return failedErr(rateLimited)
}

func (p *pace) prune(now time.Time) {
	cutoff := now.Add(-p.window)
	n := 0
	for _, ts := range p.stamps {
		if ts.After(cutoff) {
			p.stamps[n] = ts
			n++
		}
	}
	p.stamps = p.stamps[:n]
}

func deadlineAllows(ctx context.Context, now time.Time, wait time.Duration) bool {
	dl, ok := ctx.Deadline()
	if !ok {
		return true
	}
	return !now.Add(wait).After(dl)
}

// allowedRead is the collect allowlist. Anything else, including secrets,
// repositories, usage, a single run, and the SSE stream, is refused before
// a request is built.
func allowedRead(path string) bool {
	switch path {
	case "/v1/me", "/v1/agents":
		return true
	}
	rest, ok := strings.CutPrefix(path, "/v1/agents/")
	if !ok {
		return false
	}
	id, ok := strings.CutSuffix(rest, "/runs")
	if !ok || strings.Contains(id, "/") {
		return false
	}
	return safeID(id)
}

func (s *Source) get(ctx context.Context, p *pace, token, path, rawQuery string) ([]byte, error) {
	if !allowedRead(path) {
		return nil, failedErr("refusing non-read Cursor route")
	}
	for attempt := 0; attempt < len(retryBackoffs)+1; attempt++ {
		if err := p.wait(ctx); err != nil {
			return nil, err
		}
		body, status, retry, hasRetry, err := s.do(ctx, token, path, rawQuery)
		if err != nil {
			return nil, err
		}
		if status != http.StatusTooManyRequests {
			return interpret(status, body)
		}
		if attempt == len(retryBackoffs) {
			return nil, failedErr(rateLimited)
		}
		wait := retryBackoffs[attempt]
		if hasRetry {
			wait = retry
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !deadlineAllows(ctx, p.now(), wait) {
			return nil, failedErr(rateLimited)
		}
		if err := p.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
	return nil, failedErr(rateLimited)
}

func (s *Source) do(ctx context.Context, token, path, rawQuery string) ([]byte, int, time.Duration, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, false, err
	}
	endpoint, err := s.requestURL(path, rawQuery)
	if err != nil {
		return nil, 0, 0, false, failedErr("cannot create Cursor request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, false, failedErr("cannot create Cursor request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, 0, false, ctx.Err()
		}
		return nil, 0, 0, false, failedErr("Cursor request failed: " + scrub(err.Error(), token))
	}
	defer func() { _ = resp.Body.Close() }()

	limit := s.maxBody()
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, 0, false, ctx.Err()
		}
		return nil, 0, 0, false, failedErr("reading Cursor response: " + scrub(err.Error(), token))
	}
	if len(body) > limit {
		return nil, 0, 0, false, failedErr("Cursor response is too large")
	}
	retry, hasRetry := retryAfter(resp.Header, s.clockNow())
	return body, resp.StatusCode, retry, hasRetry, nil
}

func (s *Source) clockNow() time.Time {
	now, _ := s.clock()
	return now()
}

func (s *Source) requestURL(path, rawQuery string) (string, error) {
	base, err := url.Parse(s.endpointURL())
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("bad endpoint")
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = rawQuery
	base.Fragment = ""
	base.User = nil
	return base.String(), nil
}

func interpret(status int, body []byte) ([]byte, error) {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, failedErr(authRejected)
	}
	if status < 200 || status >= 300 {
		return nil, failedErr(fmt.Sprintf("Cursor returned HTTP %d", status))
	}
	if !json.Valid(body) {
		return nil, failedErr("Cursor returned invalid JSON")
	}
	return body, nil
}

func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			secs = 0
		}
		if secs > 86400 {
			secs = 86400
		}
		return time.Duration(secs) * time.Second, true
	}
	when, err := http.ParseTime(v)
	if err != nil {
		return 0, false
	}
	d := when.Sub(now)
	if d < 0 {
		d = 0
	}
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	return d, true
}

func scrub(text, secret string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

type wireAgent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updatedAt"`
}

type wireRun struct {
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
	Git       *wireGit `json:"git"`
}

type wireGit struct {
	Branches []wireBranch `json:"branches"`
}

type wireBranch struct {
	RepoURL string `json:"repoUrl"`
	Branch  string `json:"branch"`
	PRURL   string `json:"prUrl"`
}

func (s *Source) me(ctx context.Context, p *pace, token string) (string, []source.LogLine, error) {
	body, err := s.get(ctx, p, token, "/v1/me", "")
	if err != nil {
		return "", nil, err
	}
	var info struct {
		UserEmail string `json:"userEmail"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", nil, failedErr("Cursor returned invalid JSON")
	}
	email := strings.TrimSpace(info.UserEmail)
	if email == "" {
		return "", []source.LogLine{warn("service-account key has no userEmail")}, nil
	}
	if strings.IndexFunc(email, func(r rune) bool {
		return r <= ' ' || r == 0x7f
	}) >= 0 {
		return "", []source.LogLine{warn("userEmail was ignored")}, nil
	}
	return email, nil, nil
}

func (s *Source) listAgents(ctx context.Context, p *pace, token string, since time.Time) ([]agent, []source.LogLine, error) {
	var (
		cursor  string
		kept    []agent
		skipped int
		seen    = map[string]struct{}{}
	)
	for page := 0; page < s.pages(); page++ {
		if err := ctx.Err(); err != nil {
			return nil, skipLogs(skipped, "malformed agent", "malformed agents"), err
		}
		body, err := s.get(ctx, p, token, "/v1/agents", pageQuery(cursor))
		if err != nil {
			return nil, skipLogs(skipped, "malformed agent", "malformed agents"), err
		}
		items, next, err := decodePage(body)
		if err != nil {
			return nil, skipLogs(skipped, "malformed agent", "malformed agents"), err
		}
		if len(items) == 0 {
			return kept, skipLogs(skipped, "malformed agent", "malformed agents"), nil
		}
		var parsed []time.Time
		for _, raw := range items {
			item, ts, ok := decodeAgent(raw)
			if !ok {
				skipped++
				continue
			}
			parsed = append(parsed, ts)
			if ts.Before(since) {
				continue
			}
			if _, dup := seen[item.ID]; dup {
				continue
			}
			seen[item.ID] = struct{}{}
			kept = append(kept, item)
		}
		if allBefore(parsed, since) || next == "" {
			return kept, skipLogs(skipped, "malformed agent", "malformed agents"), nil
		}
		if next == cursor {
			return nil, skipLogs(skipped, "malformed agent", "malformed agents"), failedErr("Cursor agent page cursor did not advance")
		}
		cursor = next
	}
	return nil, skipLogs(skipped, "malformed agent", "malformed agents"), failedErr(fmt.Sprintf("Cursor agents exceeded %d pages", s.pages()))
}

func (s *Source) listRuns(ctx context.Context, p *pace, token, agentID string, since, until time.Time) ([]run, int, error) {
	if !safeID(agentID) {
		return nil, 0, failedErr("Cursor agent id is invalid")
	}
	path := "/v1/agents/" + url.PathEscape(agentID) + "/runs"
	var (
		cursor  string
		kept    []run
		skipped int
		seen    = map[string]struct{}{}
	)
	for page := 0; page < s.pages(); page++ {
		if err := ctx.Err(); err != nil {
			return nil, skipped, err
		}
		body, err := s.get(ctx, p, token, path, pageQuery(cursor))
		if err != nil {
			return nil, skipped, err
		}
		items, next, err := decodePage(body)
		if err != nil {
			return nil, skipped, err
		}
		if len(items) == 0 {
			return kept, skipped, nil
		}
		var parsed []time.Time
		for _, raw := range items {
			item, ok := decodeRun(raw)
			if !ok {
				skipped++
				continue
			}
			parsed = append(parsed, item.CreatedAt)
			if item.CreatedAt.Before(since) || !item.CreatedAt.Before(until) {
				continue
			}
			if _, dup := seen[item.ID]; dup {
				continue
			}
			seen[item.ID] = struct{}{}
			kept = append(kept, item)
		}
		if allBefore(parsed, since) || next == "" {
			return kept, skipped, nil
		}
		if next == cursor {
			return nil, skipped, failedErr("Cursor run page cursor did not advance")
		}
		cursor = next
	}
	return nil, skipped, failedErr(fmt.Sprintf("Cursor runs for %s exceeded %d pages", agentID, s.pages()))
}

func pageQuery(cursor string) string {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(pageSize))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return q.Encode()
}

func decodePage(body []byte) ([]json.RawMessage, string, error) {
	var page struct {
		Items *[]json.RawMessage `json:"items"`
		Next  *string            `json:"nextCursor"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", failedErr("Cursor returned invalid JSON")
	}
	if page.Items == nil {
		return nil, "", failedErr("Cursor response is missing items")
	}
	next := ""
	if page.Next != nil {
		next = strings.TrimSpace(*page.Next)
	}
	return *page.Items, next, nil
}

func decodeAgent(raw json.RawMessage) (agent, time.Time, bool) {
	var wire wireAgent
	if err := json.Unmarshal(raw, &wire); err != nil {
		return agent{}, time.Time{}, false
	}
	ts, ok := parseTime(wire.UpdatedAt)
	if !ok || !safeID(wire.ID) {
		return agent{}, time.Time{}, false
	}
	return agent{
		ID:        wire.ID,
		Name:      wire.Name,
		URL:       wire.URL,
		UpdatedAt: ts,
	}, ts, true
}

func decodeRun(raw json.RawMessage) (run, bool) {
	var wire wireRun
	if err := json.Unmarshal(raw, &wire); err != nil {
		return run{}, false
	}
	created, ok := parseTime(wire.CreatedAt)
	if !ok || !safeID(wire.ID) {
		return run{}, false
	}
	item := run{
		ID:        wire.ID,
		Status:    wire.Status,
		CreatedAt: created,
	}
	if ts, ok := parseTime(wire.UpdatedAt); ok {
		item.UpdatedAt = ts
		item.HasUpdate = true
	}
	if wire.Git != nil {
		for _, b := range wire.Git.Branches {
			item.Branches = append(item.Branches, branch{
				RepoURL: b.RepoURL,
				Name:    b.Branch,
				PRURL:   b.PRURL,
			})
		}
	}
	return item, true
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func allBefore(times []time.Time, since time.Time) bool {
	if len(times) == 0 {
		return false
	}
	for _, ts := range times {
		if !ts.Before(since) {
			return false
		}
	}
	return true
}
