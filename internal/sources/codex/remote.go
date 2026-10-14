package codex

import (
	"net/url"
	"strings"
)

// normalizeRemote returns host/path with the user, port, and .git suffix removed.
// The host is lowercased. A local path or file URL has no normalized remote.
// Userinfo is dropped so a token in the URL is not stored.
func normalizeRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		if host, path, ok := splitSCP(raw); ok {
			return joinRemote(host, path)
		}
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	if strings.EqualFold(parsed.Scheme, "file") {
		return ""
	}
	return joinRemote(parsed.Hostname(), parsed.EscapedPath())
}

func splitSCP(raw string) (host, path string, ok bool) {
	if strings.Contains(raw, `\`) {
		return "", "", false
	}
	userHost, path, found := strings.Cut(raw, ":")
	if !found || path == "" || strings.Contains(userHost, "/") {
		return "", "", false
	}
	host = userHost
	if at := strings.LastIndex(userHost, "@"); at >= 0 {
		host = userHost[at+1:]
	}
	if host == "" || strings.Contains(host, ":") {
		return "", "", false
	}
	if !strings.Contains(host, ".") && host != "localhost" {
		return "", "", false
	}
	return host, path, true
}

func joinRemote(host, path string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	path = strings.TrimPrefix(path, "/")
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if host == "" || path == "" || strings.Contains(host, "/") {
		return ""
	}
	return host + "/" + path
}
