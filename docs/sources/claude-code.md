# Claude Code

See [Sources](../SOURCES.md).

`claude-code` reads `$CLAUDE_CONFIG_DIR/projects`, or `~/.claude/projects` when that variable is unset. Use Claude Code at least once so it writes a session. No token is required. The source table accepts no keys besides `type`.

The on-disk format is not a public schema. The parser keeps the fields it understands and skips lines it does not. Sub-agent files fold into the parent session.

```bash
weeklog collect --source claude-code
```
