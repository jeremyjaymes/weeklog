# Cursor cloud agent and Grok Bot activity

Research for [#57](https://github.com/jeremyjaymes/weeklog/issues/57). Sources were the public Cursor docs and the staff replies cited below, read on 2026-10-16. The built-in `cursor-cloud` collector implements the sketch in this note.

Two products were in scope.

| Product | Pull API weeklog can call on a schedule | Verdict |
|---|---|---|
| Cursor Cloud Agents (the Background Agents API, now the Cloud Agents API) | Yes. `https://api.cursor.com/v1/agents` and `…/runs` | Viable. Sketch and follow-up ticket below. |
| Grok Bot turns, prompts, and actions | No personal pull API | Not viable. GitHub signals cover cloud-agent git output. They do not cover Bot chats. |

## Cursor Cloud Agents

The current API is [Cloud Agents API v1](https://cursor.com/docs/cloud-agent/api/endpoints). It is in public beta, available on all plans, and the docs say the shape can change before general availability. v1 models a durable agent (`bc-…`) plus one run per prompt (`run-…`). The legacy [v0 API](https://cursor.com/docs/cloud-agent/api/v0) is still up. New work should use v1. v1 webhooks are described as coming soon. v0 still has webhooks. A weekly collect is a pull, so it does not need them.

Base URL: `https://api.cursor.com`.

### Reads that map to a week

List responses are newest first. `nextCursor` is omitted when the page is the last one. Treat a missing `nextCursor` as the end. It is not returned as `null`.

| Call | Role in a collect |
|---|---|
| `GET /v1/me` | Names the key. A user key includes `userEmail`. A service-account key does not. |
| `GET /v1/agents?limit=100` | Agents for the authenticated principal. Each item has `id`, `name`, `status`, `url`, `createdAt`, `updatedAt`, `latestRunId`. Items omit `repos`. `includeArchived` defaults to true, which keeps finished agents in the list. `prUrl` filters to one pull request. There is no time filter. |
| `GET /v1/agents/{id}/runs?limit=100` | Runs for one agent. Items include `id`, `agentId`, `status`, `createdAt`, `updatedAt`, and `git` once a branch exists. |
| `GET /v1/agents/{id}/usage` | Optional. Token counts per run: `inputTokens`, `outputTokens`, `cacheWriteTokens`, `cacheReadTokens`, `totalTokens`. |

`git.branches[]` entries are `{ repoUrl, branch?, prUrl? }`. `repoUrl` has no scheme (`github.com/org/repo`), which is the form weeklog already stores. The docs are explicit that `git` is the agent's current snapshot: every run on that agent returns the same branches. `latestRunId` says which run is current. Older runs still need their own events, and their branch fields are the agent's current branch.

Run status values used by the docs: `CREATING`, `RUNNING`, `FINISHED`, `ERROR`, `CANCELLED`, `EXPIRED`. `durationMs` and `result` appear on `GET /v1/agents/{id}/runs/{runId}` for a terminal run. `result` is the final assistant reply. List and get for the agent do not return the user prompt. The agent `name` is derived from that prompt when the caller did not set one.

`GET /v1/agents/{id}/runs/{runId}/stream` is a Server-Sent Events stream of status, assistant text, thinking, and tool calls. That is a live tail, not a weekly export.

### Auth

The API accepts [Basic or Bearer](https://cursor.com/docs/api). Both schemes are documented as equivalent.

```text
Authorization: Bearer <key>
Authorization: Basic base64("<key>:")
```

The key is a user API key from Cursor Dashboard → API Keys, or a service-account API key from team settings. [Service accounts](https://cursor.com/docs/account/enterprise/service-accounts) are team automations. The dashboard shows a new key once.

A user key and a service-account key see different agents. `GET /v1/me` on a user key returns `userId` and `userEmail`. On a service-account key those fields are omitted, and the agents it lists are that account's agents. Interactive runs a person started from the IDE or the web are on the user key. Runs started with a service-account key are on that account.

There is no documented read-only scope. The key that can list runs can also create agents, send follow-ups, cancel runs, and delete agents. The secrets routes on the same host accept the same user and service-account keys. Their responses list names, types, and repositories, and the docs say no response includes a secret value. A create or update sends the value. A collect must not call those routes. On the secrets routes, team API keys and automation webhook API keys are rejected, and a key that starts with `grok_` receives `403 key_not_supported`.

Jeremy's preference for this source is one literal token in `config.toml`: a single `token` field, and no second credential. That matches weeklog's current secret form. A literal value is normal. `config check` reports the field as `literal` and does not print it. On Unix, a config file that is group- or world-readable is a warning. `weeklog init` writes the file with mode 0600. `env:`, `cmd:`, and `file:` still work and are optional. This sketch does not use them.

### Rate limits

The [API overview](https://cursor.com/docs/api) sets a default of 20 requests per minute per authenticated user, team, or organization, scoped to an endpoint, unless that endpoint publishes a different number. The Cloud Agents row in that table says "Standard rate limiting" and does not publish a separate number. Plan on the default of 20 per minute per endpoint.

Documented exceptions a collect must not trip:

| Endpoint | Limit |
|---|---|
| `GET /v1/repositories` | 1 request per user per minute, and 30 per user per hour. The call can take tens of seconds. |
| Worker claim-token minting | 30 per minute per agent, with `Retry-After` on HTTP 429 |

HTTP 429 means the limit was exceeded. Admin and Organization responses are documented to include `Retry-After: 60`. The Cloud Agents section does not repeat that header. Honor `Retry-After` when it is present, and otherwise back off. The plugin process is killed at 120 seconds (`--timeout` can be shorter). At 20 requests per minute, a collect that also calls Get A Run and Get Usage for every run will run out of clock on a busy week. The sketch below stays on List Agents and List Runs.

### Privacy

Cloud Agents store the repository, the conversation, and tool output in Cursor's cloud. That storage is required for the feature. [Privacy Mode](https://cursor.com/docs/cloud-agent/security) means Cursor does not train on that code or on the prompts and responses. It does not mean the API withholds them from the key. Legacy Privacy Mode is incompatible with Cloud Agents because it refuses cloud storage.

[Data retention](https://cursor.com/docs/cloud-agent/security-network): conversation history is kept indefinitely by default so a run can be resumed. Environment snapshots roll off after 30 days without use. `DELETE /v1/agents/{id}` removes the transcript and artifacts. Enterprise teams can cap conversation retention. Most models run under Cursor's zero-data-retention agreements with the provider. A few models, called out in [Privacy and data governance](https://cursor.com/docs/enterprise/privacy-and-data-governance), retain inputs and outputs at the provider until an admin approves that retention.

What a weeklog event would copy into SQLite:

| Field | Store it |
|---|---|
| Agent id, run id, name, status, timestamps, repo, branch, pull request URL | Yes. These are the activity record. |
| Token counts from `GET /v1/agents/{id}/usage` | Safe to store later. This sketch skips the call so a week stays inside 20 requests per minute and the 120 second plugin limit. |
| `result`, the SSE stream, thinking, tool arguments, tool results, artifact bytes | Leave them on Cursor's side. `result` and tool output can contain source, paths, and secrets that weeklog's redaction patterns miss. Redaction covers known token shapes. It leaves a source file in place. |
| Cloud Agent secret names or values | Leave them. The secrets routes list names and take values on write. A collect does not call them. |

The token in `config.toml` is a write-capable credential on disk. weeklog does not print it. The file still holds it. Keep the file out of git. A copy of the file can list and start that principal's agents, cancel and delete them, and write Cloud Agent secrets.

A user key lists that user's agents. It does not list teammates' agents. A service-account key lists the service account's agents and is the wrong token for a personal week.

### Plugin sketch

Executable name: `weeklog-source-cursor-cloud`. Protocol name: `cursor-cloud`. The collector is the built-in source in `internal/sources/cursorcloud`. weeklog serves it under that executable name. It is not a separate program on `PATH`. The Python below is the sketch of that collector, not an installed plugin. `examples/weeklog-source-todoist` remains the working example of an external plugin.

`describe` accepts one key:

```json
{
  "protocol": 1,
  "name": "cursor-cloud",
  "version": "0.1.0",
  "config": {
    "token": {"type": "secret", "required": true}
  }
}
```

Config, with the literal token Jeremy asked for:

```toml
[[source]]
type = "cursor-cloud"
token = "crsr_…"
```

weeklog resolves that value and passes the plain string on stdin. The plugin process does not inherit the parent environment. The sketch sends `Authorization: Bearer` plus the token. It writes the token nowhere.

Collect:

1. Reject a collect whose `protocol` is not 1, whose `token` is empty, or whose window is invalid. Exit 2.
2. `GET /v1/me`. Remember `userEmail` as `actor` when it is present. A response with no `userEmail` is a service-account key. Log a warning and continue. Those events are that account's runs.
3. Page `GET /v1/agents?limit=100`. Keep an agent whose `updatedAt` is at or after `since`. An agent updated after `until` can still hold a run inside the window, so the upper bound is applied to runs, not to agents. Stop paging once a whole page is older than `since`, because the list is newest first. A follow-up moves `updatedAt`, which is why an old agent with a new run stays on the recent pages.
4. For each kept agent, page `GET /v1/agents/{id}/runs?limit=100`. Keep a run whose `createdAt` is inside the window. Stop paging that agent once a whole page is older than `since`.
5. Emit one `agent_session` per kept run. Skip `GET /v1/agents/{id}`, Get A Run, usage, artifacts, repositories, secrets, and the SSE stream.
6. On HTTP 401 or 403, exit 1 with `Cursor rejected the token`. On HTTP 429, honor `Retry-After` when present and otherwise wait 1s, 2s, 4s. If the remaining time cannot finish the window, exit 1. Exit 0 only after every in-window agent and run has been read. A failed collect keeps none of its events, which is the protocol rule, so a partial week is not stored as a complete one.
7. Emit no cursor. The next collect reads the window again. Dedupe is `(source, source_id)`.

Event fields:

| Field | Value |
|---|---|
| `source_id` | `{agentId}/{runId}` |
| `kind` | `agent_session` |
| `ts` | Run `createdAt` |
| `ts_end` | Run `updatedAt` when it is at or after `ts`. This is the list timestamp, not `durationMs`. |
| `actor` | `userEmail` from `/v1/me`, when the key has one |
| `title` | Agent `name` |
| `url` | Pull request URL when `git` has one, otherwise the agent `url` (`https://cursor.com/agents/bc-…`) |
| `repo` | First `git.branches[].repoUrl` |
| `branch` | First `git.branches[].branch` |
| `refs` | Each `prUrl` reduced to `#N`, plus the branch |
| `state` | `CREATING` and `RUNNING` → `open`. `FINISHED` → `done`. `ERROR`, `CANCELLED`, and `EXPIRED` → `abandoned` |
| `summary` | Omitted. The assistant `result` is not copied. |

`git` on a run is the agent's current snapshot. The follow-up implementation should keep that in mind when a later run has moved the branch: re-collecting an old run can refresh `branch` and `url` to the agent's latest values. `source_id` stays on the run, so the row is updated in place rather than duplicated.

The sketch, abbreviated at the HTTP edge:

```python
#!/usr/bin/env python3
"""Sketch of weeklog-source-cursor-cloud. Not an installed plugin."""

import json
import sys
import urllib.request

API = "https://api.cursor.com"


def describe():
    json.dump({
        "protocol": 1,
        "name": "cursor-cloud",
        "version": "0.1.0",
        "config": {"token": {"type": "secret", "required": True}},
    }, sys.stdout)
    sys.stdout.write("\n")


def emit(event):
    json.dump({"type": "event", "event": event}, sys.stdout)
    sys.stdout.write("\n")


def collect():
    req = json.load(sys.stdin)
    if req.get("protocol") != 1:
        return 2
    token = (req.get("config") or {}).get("token") or ""
    if not token.strip():
        sys.stderr.write("token is required\n")
        return 2
    # GET /v1/me, then page /v1/agents and /v1/agents/{id}/runs.
    # Bearer auth. Pace for 20 requests/minute. Never call /secrets,
    # /repositories, or the run stream. Leave summary unset.
    me = get(token, "/v1/me")
    actor = me.get("userEmail") or ""
    for agent in agents_touched_since(token, req["since"]):
        for run in runs_started_in_window(token, agent["id"], req["since"], req["until"]):
            emit(session_event(agent, run, actor))
    return 0


def get(token, path):
    request = urllib.request.Request(
        API + path,
        headers={"Authorization": "Bearer " + token, "Accept": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def session_event(agent, run, actor):
    branches = (run.get("git") or {}).get("branches") or []
    first = branches[0] if branches else {}
    event = {
        "source_id": agent["id"] + "/" + run["id"],
        "kind": "agent_session",
        "ts": run["createdAt"],
        "title": agent.get("name") or "",
        "url": first.get("prUrl") or agent.get("url") or "",
        "repo": first.get("repoUrl") or "",
        "branch": first.get("branch") or "",
        "state": run_state(run.get("status")),
    }
    if actor:
        event["actor"] = actor
    if run.get("updatedAt"):
        event["ts_end"] = run["updatedAt"]
    return event


def run_state(status):
    if status in ("CREATING", "RUNNING"):
        return "open"
    if status == "FINISHED":
        return "done"
    return "abandoned"
```

The two helpers are the paging described above. The HTTP bodies are omitted so this file is not a second client.

### Attribution gap

[ATTRIBUTION.md](../ATTRIBUTION.md) names a session actor from `claude-code`, `claude`, `codex`, and `cursor-cloud`. `cursor-cloud` is Cloud agent. A session with no commit follows rule 3 and is Cloud agent. A session linked to a commit follows rule 2 and is Cloud agent. Rule 1 still wins when the commit or pull request carries `cursoragent@cursor.com`.

The catalog row shipped with the source. Without it, a linked session is an unlisted agent: rule 2 returns Unknown and the later `cursor/` branch rule does not run. That would relabel items that are Cloud agent from the branch alone.

### Follow-up ticket

Filed as [#69](https://github.com/jeremyjaymes/weeklog/issues/69). The built-in implements it.

Title: Add a cursor-cloud source plugin

```text
Implement the weeklog-source-cursor-cloud sketch in
docs/research/agent-activity-source.md.

Config is one literal token:

  [[source]]
  type = "cursor-cloud"
  token = "crsr_…"

Reads, all on https://api.cursor.com, with Bearer auth:

- GET /v1/me
- GET /v1/agents
- GET /v1/agents/{id}/runs

Do not call create, cancel, delete, secrets, repositories, usage, or the
SSE stream in this change. Do not store run result text.

Emit one agent_session per run whose createdAt is in the window.
source_id is {agentId}/{runId}. Exit 1 on auth failure or when the 20
requests/minute budget cannot finish the window inside the plugin
timeout. Exit 0 only for a complete window.

Add cursor-cloud to the attribution session catalog as Cloud agent in
the same change. Without that row, a linked session becomes Unknown
and hides the cursor/ branch rule.

Refs #57.
```

## Grok Bot

Grok Bot activity (turns, prompts, shell commands, browser navigation, tool calls) has no pull API a personal weeklog can poll with one token.

What does exist, and why each one is a different product:

| Surface | What it returns | Why it is not a weeklog source |
|---|---|---|
| [Organization API `POST/GET /organizations/teams/{teamId}/grok-bot/operations`](https://cursor.com/docs/account/organizations/organization-admin-api) | Recreate, terminate, or delete of members' Bot computers, then a per-member result | Computer lifecycle for org admins. One operation per team at a time. Scope `admin:*` only. |
| [Admin API Grok Bot routes](https://cursor.com/docs/account/teams/admin-api) | Enablement, capabilities, access, network policy, team rules, setup manifests | Team configuration. |
| [Admin API `GET /teams/audit-logs`](https://cursor.com/docs/account/teams/admin-api) | Control-plane audit events. `application_type` is `grok_bot` for Grok Bot. Event types include `grok_bot_created`, `grok_bot_lifecycle`, `grok_bot_routine`, `grok_bot_vm`, and the other rows in [Compliance and monitoring](https://cursor.com/docs/enterprise/compliance-and-monitoring). | Enterprise admin key. A query window is at most 30 days. 20 requests per minute per team. The log records who changed a setting. [Compliance](https://cursor.com/docs/enterprise/compliance-and-monitoring) states that prompts, agent output, generated code, and Bot actions such as shell commands are outside this log. `grok_bot_routine` is a routine being created or edited, not a routine firing. |
| [Action Recording](https://cursor.com/docs/grok-bot/security) plus [OpenTelemetry Export](https://cursor.com/docs/enterprise/opentelemetry-export) | Push of `cursor.grok_bot.*` logs to a collector the team runs. Optional `conversation_content` adds scrubbed prompts and responses for Cloud Agents and Grok Bot. | Enterprise. Action Recording is off by default. Privacy Mode (Legacy) forces it off. Cursor pushes OTLP to the team's endpoint. There is no Cursor URL weeklog can GET. Export does not backfill. Delivery of logs is at-least-once. The internal Action Recording store retains 90 days. |
| In-bot `ReadTranscript` | A Bot can read its own history, up to 200 messages per page, newest first | Not an HTTP API. A Cursor staff reply on 2026-10-12 says there is no export button and no public API for Grok Bot chat history, and that a deleted Bot's history cannot be read this way. |

The Grok coding CLI (`grok sessions`, `grok export`, files under `~/.grok/sessions/`) is a local transcript for that CLI. It is a different surface from Grok Bot and is outside this ticket.

### Auth, limits, and privacy of the nearby APIs

These are recorded so a later reader does not treat them as an activity feed.

Organization and Admin calls use Basic auth with an organization or team API key. Grok Bot computer operations accept only `admin:*`. Other organization routes accept narrower scopes (`members:read`, `usage:*`, and so on). The operations GET routes are documented at 120 requests per minute per endpoint. Other organization routes and `GET /teams/audit-logs` are 20 per minute. Audit log responses include `ip_address` and `user_email`. Action Recording exports scrub secrets on a best-effort basis and still can carry shell text (capped at 8 KiB) and, when the team opts in, prompt and response text (capped at 32 KiB). That stream is for a team collector, not for a personal SQLite file.

### GitHub-signal fallbacks

Until a Bot export exists, weeklog keeps using GitHub. The three signals below are real, and weeklog already uses the first two for Cloud agent attribution. They describe commits and pull requests Cursor's agent pushed. A Grok Bot turn that never pushes a branch and never opens a pull request does not produce them. A Bot that hands work to a cloud agent shows up only as that cloud agent's git output.

#### Branch names

With `workOnCurrentBranch` left at its default of false, the Cloud Agents API pushes a new branch. The documented shape is `cursor/…`. The dashboard can replace the prefix with one static string. An empty prefix falls back to `cursor/`. The prefix cannot be turned off, and it is chosen before the agent reads repository branch rules. `workOnCurrentBranch: true` pushes onto the branch the run was pointed at, which often has no `cursor/` prefix.

weeklog already maps the first path segment `cursor` to Cloud agent ([ATTRIBUTION.md](../ATTRIBUTION.md) rule 4). `cursor` with no slash does not match. A custom prefix does not match. The linker also joins a pull request to commits and sessions on the same head branch in the same repository (`branch:<name>`), for any prefix.

#### Co-authored-by

Two different trailers show up, depending on who authored the commit.

| Surface | Author | Trailer |
|---|---|---|
| IDE and CLI agent, attribution left on | The person | `Co-authored-by: Cursor <cursoragent@cursor.com>`. The [Git help](https://cursor.com/help/integrations/git) also describes a `Made with Cursor` trailer on commits and pull requests. Settings → Git & PRs → Attribution can turn commit and pull request attribution off. An enterprise admin can turn it off for the team. weeklog's attribution reads `Co-authored-by` lines. A `Made with Cursor` line with no `Co-authored-by` is not one of those lines. |
| Hosted Cloud Agent | `Cursor Agent <cursoragent@cursor.com>` | `Co-authored-by` names the person who started the run. Cursor staff described this direction in the git-attribution thread (the IDE toggle does not flip the cloud-agent author). A commit made with `--no-verify`, or a repo hook that replaces the agent hook, can omit the trailer. |

weeklog's git collector stores the commit subject and body, then the store caps `summary` at 500 characters. Attribution reads `Co-authored-by` in the stored title and summary, and it treats actor `cursoragent@cursor.com`, the name `Cursor` / `Cursor Agent` at a `cursor.com` mailbox, and GitHub logins `cursoragent` and `cursor[bot]` as Cloud agent. A human trailer is ignored. A trailer past the 500-character cap is gone before attribution runs. That limit is already stated in ATTRIBUTION.md.

The git collector keeps a commit when the author name or email matches `authors` (or `identity.emails` when `authors` is omitted). A Cloud Agent commit authored as `cursoragent@cursor.com` is dropped when `authors` is only the person's address. The pull request can still be collected: Cursor staff have said an interactive cloud-agent pull request is opened by the person's GitHub account, and that automations and service-account runs open the pull request as `cursor[bot]`. weeklog already treats the login `cursor[bot]` as Cloud agent when that identity is stored on the event.

#### PR footer

Cloud agent pull requests gain a footer the agent rewrites when it updates the body. Cursor staff said on 2026-07-13 that it cannot be disabled. The buttons are separate from the "Made with Cursor" attribution line, which can be disabled.

The footer observed on this repository's pull requests is HTML at the end of the body: an "Open in Web" anchor to `https://cursor.com/agents/bc-<id>` and an "Open in Cursor" anchor to `https://cursor.com/background-agent?bcId=bc-<id>`, with `cursor_ref=pr_footer`. The `bc-` id in those URLs is the Cloud Agent id from the API.

weeklog's GitHub collector stores the pull request body and extracts ticket keys from the full body. The stored summary is then capped at 500 characters, so a footer on a long body is not in the text attribution reads. Nothing today matches "Made with Cursor", "Open in Web", or `cursor.com/agents/bc-`. The reliable stored signals remain the `cursor/` branch and a `Co-authored-by` / `cursoragent` identity that survived the summary cap.

Using the footer as a fourth attribution rule would recover the `bc-` id on short bodies and would still miss it on long ones. The API sketch stores that id as `source_id` without parsing HTML.

## Recommendation

The built-in `cursor-cloud` source follows the sketch, with the attribution catalog change, and with the single literal `token` above.

Leave Grok Bot off the source list. Keep attributing cloud-agent git output from `cursor/` branches and `Co-authored-by` / `cursoragent` identities. Revisit Grok Bot when Cursor publishes a pull API for Bot turns. OpenTelemetry Export is a team push pipeline, not that API.
