// Package claudecode collects Claude Code session activity for weeklog.
//
// Transcripts live under $CLAUDE_CONFIG_DIR/projects, or ~/.claude/projects
// when that variable is unset. The on-disk format is not a public schema, so
// the parser keeps the fields it understands and skips lines it does not.
// Sub-agent files fold into the parent session.
package claudecode
