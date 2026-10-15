package source

import (
	"net/url"
	"strings"
)

// LoopbackEndpoint returns config["endpoint"] when it is an http URL on the
// loopback interface. The key is not part of any built-in describe schema, and
// weeklog config check rejects it in a user's TOML file. Collectors honor it
// so a test can serve recorded HTTP responses to the in-process source and to
// the same source running through the subprocess adapter.
//
// Any other value is ignored. The plugin environment does not carry arbitrary
// variables, and the collect request is the only channel both paths share.
// Restricting the override to loopback http keeps a config value from sending
// a token to another host.
func LoopbackEndpoint(config map[string]any) (string, bool) {
	if config == nil {
		return "", false
	}
	raw, ok := config["endpoint"].(string)
	if !ok {
		return "", false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1":
		return raw, true
	default:
		return "", false
	}
}
