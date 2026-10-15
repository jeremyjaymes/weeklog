// Package github collects pull requests, reviews, and issues from GitHub.
//
// Collect calls the GraphQL API for the configured login. The token comes
// from the source config. When token is unset, Collect runs `gh auth token`.
// A pull request is kept when it was opened, closed, or merged in the window,
// or when the login submitted a review in the window. Issues are kept when
// the login authored or is assigned to them and they were opened, closed, or
// updated in the window. The event time is that in-window activity. Tests
// replay recorded responses and do not call GitHub.
//
// Collect reads config key "endpoint" only when the value is an http URL on
// 127.0.0.1, localhost, or ::1. The key is not part of the describe schema.
// config check accepts that loopback form and rejects any other value. It
// exists so the in-process collector, the subprocess adapter, and the real
// binary can share one local fixture server. Collect ignores a value that is
// not loopback http.
package github
