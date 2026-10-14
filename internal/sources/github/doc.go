// Package github collects pull requests, reviews, and issues from GitHub.
//
// Collect calls the GraphQL API for the configured login. The token comes
// from the source config. When token is unset, Collect runs `gh auth token`.
// A pull request is kept when it was opened, closed, or merged in the window,
// or when the login submitted a review in the window. Issues are kept when
// the login authored or is assigned to them and they were opened, closed, or
// updated in the window. The event time is that in-window activity. Tests
// replay recorded responses and do not call GitHub.
package github
