// Package cursorcloud collects Cursor Cloud Agent runs for weeklog.
//
// Collect calls the Cloud Agents API on https://api.cursor.com. It sends
// Authorization: Bearer with the configured token. The only reads are
// GET /v1/me, GET /v1/agents, and GET /v1/agents/{id}/runs. Create, cancel,
// delete, secrets, repositories, usage, a single run, and the SSE stream
// are refused. The assistant result text is not stored.
//
// Requests stay within 20 per minute. HTTP 429 honors Retry-After when it
// is present and otherwise waits 1s, 2s, then 4s. If that wait, or the pace
// for the next request, would pass the collect deadline, Collect fails and
// emits nothing. The source does not keep a cursor. Tests replay recorded
// responses and do not call Cursor.
//
// Collect reads config key "endpoint" only when the value is an http URL on
// 127.0.0.1, localhost, or ::1. The key is not part of the describe schema.
// config check accepts that loopback form and rejects any other value. It
// exists so the in-process collector, the subprocess adapter, and the real
// binary can share one local fixture server. Collect ignores a value that is
// not loopback http.
package cursorcloud
