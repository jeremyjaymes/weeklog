# Git

See [Sources](../SOURCES.md).

`git` collects commits from repositories under `roots`. init sets `roots` to `["~/Projects"]` and `authors` to `git config --global user.email` when that value has no spaces. Each root is scanned through three directory levels. `authors` matches author emails or names. When `authors` is omitted, collect uses `identity.emails`.

The collector runs `git log`. It does not contact the remote. A remote URL is stored as `host/owner/name`.

```bash
weeklog collect --source git
```
