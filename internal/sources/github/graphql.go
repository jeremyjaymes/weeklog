package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

const (
	defaultEndpoint  = "https://api.github.com/graphql"
	searchPageSize   = 50
	commitPageSize   = 100
	reviewPageSize   = 50
	assigneePageSize = 100
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
	searchQuery = fmt.Sprintf(`
query ($q: String!, $cursor: String) {
  search(query: $q, type: ISSUE, first: %d, after: $cursor) {
    pageInfo { hasNextPage endCursor }
    nodes {
      __typename
      ... on PullRequest {
        number
        title
        body
        url
        state
        createdAt
        updatedAt
        closedAt
        mergedAt
        additions
        deletions
        author { login }
        repository { nameWithOwner }
        headRefName
        commits(first: %d) {
          pageInfo { hasNextPage endCursor }
          nodes { commit { oid } }
        }
        reviews(first: %d) {
          pageInfo { hasNextPage endCursor }
          nodes {
            databaseId
            id
            author { login }
            body
            state
            submittedAt
            url
          }
        }
      }
      ... on Issue {
        number
        title
        body
        url
        state
        createdAt
        updatedAt
        closedAt
        author { login }
        repository { nameWithOwner }
        assignees(first: %d) {
          nodes { login }
        }
      }
    }
  }
}`, searchPageSize, commitPageSize, reviewPageSize, assigneePageSize)

	commitQuery = fmt.Sprintf(`
query ($owner: String!, $name: String!, $number: Int!, $cursor: String!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      commits(first: %d, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes { commit { oid } }
      }
    }
  }
}`, commitPageSize)

	reviewQuery = fmt.Sprintf(`
query ($owner: String!, $name: String!, $number: Int!, $cursor: String!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviews(first: %d, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          databaseId
          id
          author { login }
          body
          state
          submittedAt
          url
        }
      }
    }
  }
}`, reviewPageSize)

	viewerQuery = `query { viewer { login } }`
)

type pageInfo struct {
	HasNextPage bool    `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type gqlError struct {
	Message string `json:"message"`
}

func (s *Source) endpointURL() string {
	if s != nil && s.endpoint != "" {
		return s.endpoint
	}
	return defaultEndpoint
}

// withLoopback returns s, or a copy whose GraphQL URL is a loopback endpoint
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
		return nil, failedErr("cannot encode GitHub request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpointURL(), bytes.NewReader(buf))
	if err != nil {
		return nil, failedErr("cannot create GitHub request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failedErr("GitHub request failed: " + scrub(err.Error(), token))
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, failedErr("reading GitHub response: " + scrub(err.Error(), token))
	}
	if len(body) > maxResponseBytes {
		return nil, failedErr("GitHub response is too large")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		if resp.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(string(body)), "rate limit") {
			return nil, failedErr("GitHub rate limit exceeded")
		}
		return nil, configErr("GitHub rejected the token")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, failedErr(fmt.Sprintf("GitHub returned HTTP %d", resp.StatusCode))
	}

	var decoded struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, failedErr("GitHub returned invalid JSON")
	}
	if len(decoded.Errors) > 0 {
		msg := "GitHub GraphQL error"
		if text := strings.TrimSpace(decoded.Errors[0].Message); text != "" {
			msg = scrub(text, token)
		}
		return nil, failedErr(msg)
	}
	if len(decoded.Data) == 0 || string(decoded.Data) == "null" {
		return nil, failedErr("GitHub returned an empty response")
	}
	return decoded.Data, nil
}

func (s *Source) search(ctx context.Context, token, q string) ([]json.RawMessage, error) {
	var (
		cursor string
		nodes  []json.RawMessage
	)
	for page := 0; page < maxPages; page++ {
		vars := map[string]any{"q": q, "cursor": nil}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		data, err := s.gql(ctx, token, searchQuery, vars)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Search *struct {
				PageInfo pageInfo          `json:"pageInfo"`
				Nodes    []json.RawMessage `json:"nodes"`
			} `json:"search"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return nil, failedErr("GitHub search response is invalid")
		}
		if parsed.Search == nil {
			return nil, failedErr("GitHub search returned no data")
		}
		nodes = append(nodes, parsed.Search.Nodes...)
		if !parsed.Search.PageInfo.HasNextPage {
			return nodes, nil
		}
		next := ""
		if parsed.Search.PageInfo.EndCursor != nil {
			next = strings.TrimSpace(*parsed.Search.PageInfo.EndCursor)
		}
		if next == "" || next == cursor {
			return nil, failedErr("GitHub search page is missing a cursor")
		}
		cursor = next
	}
	return nil, failedErr("GitHub search exceeded 20 pages")
}

func (s *Source) viewerLogin(ctx context.Context, token string) (string, error) {
	data, err := s.gql(ctx, token, viewerQuery, nil)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", failedErr("GitHub viewer response is invalid")
	}
	return strings.TrimSpace(parsed.Viewer.Login), nil
}

func searchDates(since, until time.Time) (string, string) {
	since = since.UTC()
	end := until.UTC().Add(-time.Nanosecond)
	return since.Format("2006-01-02"), end.Format("2006-01-02")
}

func searchText(kind, login string, since, until time.Time, pr bool) string {
	what := "is:issue"
	if pr {
		what = "is:pr"
	}
	from, to := searchDates(since, until)
	return fmt.Sprintf("%s %s:%s updated:%s..%s", what, kind, login, from, to)
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
