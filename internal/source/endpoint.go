package source

import (
	"net/url"
	"strings"
)

// LoopbackEndpoint returns config["endpoint"] when it is an http URL on the
// loopback interface. The key is not part of any built-in describe schema.
// config check accepts it only for github and linear, and only for this
// loopback form, so a test can put a recorded-response server in the config
// the real binary loads. Any other value is a config error there.
//
// Collectors ignore a value this function rejects. The plugin environment
// does not carry arbitrary variables, and the collect request is the only
// channel both the in-process source and the subprocess adapter share.
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
