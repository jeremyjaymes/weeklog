// Package git collects commit activity for weeklog.
//
// The collector shells out to git. It scans configured roots for repositories,
// reads commits in the requested window, and emits one commit event per SHA.
package git
