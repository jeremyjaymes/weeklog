package source

import "testing"

func TestLoopbackEndpoint(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]any
		want   string
		ok     bool
	}{
		{name: "nil"},
		{name: "missing", config: map[string]any{}},
		{name: "empty", config: map[string]any{"endpoint": "  "}},
		{name: "not a string", config: map[string]any{"endpoint": 1}},
		{name: "public", config: map[string]any{"endpoint": "http://example.com/graphql"}},
		{name: "https", config: map[string]any{"endpoint": "https://127.0.0.1/graphql"}},
		{name: "userinfo", config: map[string]any{"endpoint": "http://user:pass@127.0.0.1:9/"}},
		{name: "lookalike", config: map[string]any{"endpoint": "http://127.0.0.1.example.com/"}},
		{name: "all interfaces", config: map[string]any{"endpoint": "http://0.0.0.0:9/"}},
		{
			name:   "ipv4",
			config: map[string]any{"endpoint": " http://127.0.0.1:9/graphql "},
			want:   "http://127.0.0.1:9/graphql",
			ok:     true,
		},
		{
			name:   "localhost",
			config: map[string]any{"endpoint": "http://LOCALHOST:9"},
			want:   "http://LOCALHOST:9",
			ok:     true,
		},
		{
			name:   "ipv6",
			config: map[string]any{"endpoint": "http://[::1]:9/"},
			want:   "http://[::1]:9/",
			ok:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LoopbackEndpoint(tc.config)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("LoopbackEndpoint() = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
