// Package codex collects Codex session activity for weeklog.
//
// Rollouts live under $CODEX_HOME/sessions/YYYY/MM/DD, or ~/.codex/sessions
// when that variable is unset. The on-disk format is not a public schema, so
// the parser keeps the fields it understands and skips lines it does not.
package codex
