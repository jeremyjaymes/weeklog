# Redaction

Agent transcripts and commit messages can contain pasted secrets. weeklog removes them before an event is written to SQLite, and rendered text is produced from those stored fields. Redaction is always on. There is no switch that stores the original text.

The replacement is `[REDACTED:<kind>]`, so a log shows that something was removed and what it was.

## Where it runs

`UpsertEvents` redacts every string field, then validates and writes:

- `source`, `source_id`, `kind`, `actor`, `title`, `summary`, `url`, `repo`, `branch`, `cwd`, `body_ref`
- each `files` and `refs` entry
- metric names (values are numbers)

`kind` and `state` are enums. When a secret is removed from them, a known value that is still there is kept (`commit`, `merged`, and the other schema values). A `state` that would no longer fit the schema is stored as empty. The secret is not stored in either case.

`body_ref` is only a path. Raw transcripts are not copied. If the path itself contains a secret, that path is redacted too.

Plugins are not a special case. Their events go through the same write path.

## Rules

Rules run in this order. A match that is already inside `[REDACTED:...]` is left alone, and running redaction twice does not change the text.

1. **Config secrets.** Every value resolved from the config (`env:`, `cmd:`, `file:`, or a literal token) is removed wherever it appears. `config check` does not print these values. A value of 8 characters or more is removed even inside a longer word. A shorter value is removed only when it is a whole token, bounded by a non-alphanumeric character or the ends of the string. The label is `config-secret`.
2. **Known shapes**, adapted from the gitleaks rule set. The label is the kind that matched.

   | Shape | Label |
   |---|---|
   | AWS access key id (`AKIA`, `ASIA`, and the other common prefixes) | `aws-access-key` |
   | AWS secret access key assignment (40-character value) | `aws-secret` |
   | GitHub `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, and `github_pat_` | `github-token` |
   | Slack `xoxb-`, `xoxp-`, `xoxa-`, `xoxr-`, `xoxs-`, and Slack webhook URLs | `slack-token` |
   | Stripe `sk_live_`, `sk_test_`, `rk_live_`, `rk_test_` | `stripe-key` |
   | Anthropic `sk-ant-` | `anthropic-key` |
   | OpenAI `sk-` and `sk-proj-` | `openai-key` |
   | Private key blocks, including OpenSSH and PGP | `private-key` |
   | JWTs (`eyJ...`.`...`.`...`) | `jwt` |
   | `Bearer` followed by a token | `bearer-token` |
   | URL userinfo (`https://user:pass@host`). The host is kept. | `url-credential` |

3. **Assignments.** `KEY=value` where the key contains `secret`, `token`, `password`, `passwd`, `api_key` / `api-key` / `apikey`, or `private` (any case). Quoted values are included. JSON `"key": "value"` and YAML `key: value` (value at least 4 characters) are included, because pasted config usually uses those forms. The label follows the key (`password`, `token`, `secret`, `api-key`, or `private`).
4. **Extra patterns** from `[redaction] extra_patterns`. Each match becomes `[REDACTED:custom]`.
5. **High entropy.** A token of 24 or more characters with no spaces, drawn from letters, digits, and `+/=_-`, whose Shannon entropy is at least 4.0 bits per byte. The label is `high-entropy`.

Hex strings and UUIDs are not high-entropy matches. A git SHA has the same entropy as a hex-encoded secret, and the linker needs SHAs. A hex secret still matches when it is a known token or an assignment value (`api_key=` followed by hex).

The check prefers removing too much. A redacted commit subject is a nuisance. A leaked key is an incident.

## Config

```toml
[redaction]
extra_patterns = ["WKCUST-[0-9]{6}"]
allow = ["0123456789abcdef0123456789abcdef01234567"]
```

`allow` is an exact list of strings that must be left as written. An entry matches the secret itself (the token, the assignment value, or the URL password), not a surrounding sentence. `config check` rejects an empty entry and an extra pattern that does not compile.

`weeklog config check` prints how many patterns and allow entries are set. It does not print them.

## Tests

`testdata/secrets/corpus.json` describes fake secrets (prefix and length, not a contiguous token). The corpus test writes each one into every string field and fails if any secret appears in a SQLite row, in the database file, or in text rendered from the stored event.
