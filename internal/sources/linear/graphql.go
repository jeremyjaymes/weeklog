package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultEndpoint  = "https://api.linear.app/graphql"
	issuePageSize    = 50
	historyPageSize  = 50
	maxPages         = 20
	maxResponseBytes = 8 << 20
	userAgent        = "weeklog"
)

// defaultHTTP does not follow redirects, so the token stays on the GraphQL host.
var defaultHTTP = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var (
	issueQuery = fmt.Sprintf(`
query ($filter: IssueFilter, $cursor: String) {
  issues(filter: $filter, first: %d, after: $cursor, includeArchived: true, orderBy: updatedAt) {
    pageInfo { hasNextPage endCursor }
    nodes {
      __typename
      id
      identifier
      title
      description
      url
      branchName
      createdAt
      updatedAt
      completedAt
      canceledAt
      state { name type }
      creator { name displayName email }
      assignee { name displayName email }
      history(first: %d) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          createdAt
          fromState { name type }
          toState { name type }
          actor { name displayName email }
        }
      }
    }
  }
}`, issuePageSize, historyPageSize)

	historyQuery = fmt.Sprintf(`
query ($id: String!, $cursor: String!) {
  issue(id: $id) {
    history(first: %d, after: $cursor) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id
        createdAt
        fromState { name type }
        toState { name type }
        actor { name displayName email }
      }
    }
  }
}`, historyPageSize)
)

type pageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
		Type string `json:"type"`
	} `json:"extensions"`
}

func (s *Source) endpointURL() string {
	if s != nil && s.endpoint != "" {
		return s.endpoint
	}
	return defaultEndpoint
}

func (s *Source) httpClient() *http.Client {
	if s != nil && s.client != nil {
		return s.client
	}
	return defaultHTTP
}

func (s *Source) gql(ctx context.Context, token, query string, vars map[string]any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload := map[string]any{"query": query}
	if len(vars) > 0 {
		payload["variables"] = vars
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, failedErr("cannot encode Linear request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpointURL(), bytes.NewReader(buf))
	if err != nil {
		return nil, failedErr("cannot create Linear request")
	}
	// Linear personal API keys are the Authorization value. They are not Bearer tokens.
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failedErr("Linear request failed: " + scrub(err.Error(), token))
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failedErr("reading Linear response: " + scrub(err.Error(), token))
	}
	if len(body) > maxResponseBytes {
		return nil, failedErr("Linear response is too large")
	}
	return decodeGraphQL(resp.StatusCode, body, token)
}

func decodeGraphQL(status int, body []byte, token string) (json.RawMessage, error) {
	if status == http.StatusUnauthorized {
		return nil, configErr("Linear rejected the token")
	}
	if status == http.StatusTooManyRequests || (status == http.StatusForbidden && containsRateLimit(body)) {
		return nil, failedErr("Linear rate limit exceeded")
	}
	if status == http.StatusForbidden {
		return nil, configErr("Linear rejected the token")
	}

	var decoded struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	parsed := json.Unmarshal(body, &decoded) == nil
	if parsed && len(decoded.Errors) > 0 {
		switch {
		case rateLimited(decoded.Errors) || containsRateLimit(body):
			return nil, failedErr("Linear rate limit exceeded")
		case authError(decoded.Errors):
			return nil, configErr("Linear rejected the token")
		case status >= 200 && status < 300 || status == http.StatusBadRequest:
			return nil, failedErr(scrub(firstMessage(decoded.Errors), token))
		}
	}
	if status < 200 || status >= 300 {
		return nil, failedErr(fmt.Sprintf("Linear returned HTTP %d", status))
	}
	if !parsed {
		return nil, failedErr("Linear returned invalid JSON")
	}
	if len(decoded.Data) == 0 || string(decoded.Data) == "null" {
		return nil, failedErr("Linear returned an empty response")
	}
	return decoded.Data, nil
}

func (s *Source) search(ctx context.Context, token string, since, until time.Time) ([]json.RawMessage, error) {
	filter := issueFilter(since, until)
	var (
		cursor string
		nodes  []json.RawMessage
	)
	for page := 0; page < maxPages; page++ {
		vars := map[string]any{"filter": filter, "cursor": nil}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		data, err := s.gql(ctx, token, issueQuery, vars)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Issues *struct {
				PageInfo pageInfo          `json:"pageInfo"`
				Nodes    []json.RawMessage `json:"nodes"`
			} `json:"issues"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil || parsed.Issues == nil {
			return nil, failedErr("Linear issue page is invalid")
		}
		nodes = append(nodes, parsed.Issues.Nodes...)
		if !parsed.Issues.PageInfo.HasNextPage {
			return nodes, nil
		}
		next, ok := cursorOf(parsed.Issues.PageInfo)
		if !ok || next == cursor {
			return nil, failedErr("Linear issue page is missing a cursor")
		}
		cursor = next
	}
	return nil, failedErr("Linear issues exceeded 20 pages")
}

func (s *Source) pageHistory(ctx context.Context, token string, issue *issueItem) error {
	if issue == nil || !issue.History.PageInfo.HasNextPage {
		return nil
	}
	id := strings.TrimSpace(issue.ID)
	if id == "" {
		id = issue.Identifier
	}
	cursor, ok := cursorOf(issue.History.PageInfo)
	if !ok {
		return failedErr("Linear history page is missing a cursor")
	}
	for page := 0; page < maxPages; page++ {
		data, err := s.gql(ctx, token, historyQuery, map[string]any{
			"id":     id,
			"cursor": cursor,
		})
		if err != nil {
			return err
		}
		var parsed struct {
			Issue *struct {
				History historyConn `json:"history"`
			} `json:"issue"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil || parsed.Issue == nil {
			return failedErr("Linear history page is invalid")
		}
		conn := parsed.Issue.History
		issue.History.Nodes = append(issue.History.Nodes, conn.Nodes...)
		if !conn.PageInfo.HasNextPage {
			issue.History.PageInfo = conn.PageInfo
			return nil
		}
		next, ok := cursorOf(conn.PageInfo)
		if !ok || next == cursor {
			return failedErr("Linear history page is missing a cursor")
		}
		cursor = next
	}
	return failedErr(fmt.Sprintf("Linear history for %s exceeded 20 pages", issue.Identifier))
}

func issueFilter(since, until time.Time) map[string]any {
	return map[string]any{
		"updatedAt": map[string]any{
			"gte": since.UTC().Format(time.RFC3339),
			"lt":  until.UTC().Format(time.RFC3339),
		},
		"or": []any{
			map[string]any{"assignee": map[string]any{"isMe": map[string]any{"eq": true}}},
			map[string]any{"creator": map[string]any{"isMe": map[string]any{"eq": true}}},
		},
	}
}

func cursorOf(info pageInfo) (string, bool) {
	if info.EndCursor == nil {
		return "", false
	}
	cursor := strings.TrimSpace(*info.EndCursor)
	if cursor == "" {
		return "", false
	}
	return cursor, true
}

func containsRateLimit(body []byte) bool {
	return strings.Contains(strings.ToLower(string(body)), "rate limit")
}

func rateLimited(errs []gqlError) bool {
	for _, item := range errs {
		code := strings.ToUpper(strings.TrimSpace(item.Extensions.Code))
		if code == "RATELIMITED" || code == "RATE_LIMITED" {
			return true
		}
		if strings.Contains(strings.ToLower(item.Extensions.Type), "ratelimit") {
			return true
		}
		if strings.Contains(strings.ToLower(item.Message), "rate limit") {
			return true
		}
	}
	return false
}

func authError(errs []gqlError) bool {
	for _, item := range errs {
		code := strings.ToUpper(strings.TrimSpace(item.Extensions.Code))
		if code == "AUTHENTICATION_ERROR" {
			return true
		}
		if strings.Contains(strings.ToLower(item.Extensions.Type), "authentication") {
			return true
		}
		msg := strings.ToLower(item.Message)
		if strings.Contains(msg, "not authenticated") || strings.Contains(msg, "authentication required") {
			return true
		}
	}
	return false
}

func firstMessage(errs []gqlError) string {
	for _, item := range errs {
		if text := strings.TrimSpace(item.Message); text != "" {
			return text
		}
	}
	return "Linear GraphQL error"
}

func scrub(text, secret string) string {
	text = oneLine(text)
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	const max = 200
	if len(text) > max {
		text = text[:max]
	}
	return text
}

func oneLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
