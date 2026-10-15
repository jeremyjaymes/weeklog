// Package linear collects Linear issues and state history for weeklog.
//
// Collect calls the GraphQL API for issues assigned to or created by the
// authenticated user that were updated in the window, and the state changes
// in that window. The token comes from the source config. ticket_pattern
// selects the ticket keys stored on each event. When it is omitted, the
// built-in ticket shape is used. The source does not keep a cursor. Tests
// replay recorded responses and do not call Linear.
//
// Collect reads config key "endpoint" only when the value is an http URL on
// 127.0.0.1, localhost, or ::1. The key is not part of the describe schema.
// It exists so the in-process collector and the subprocess adapter can share
// one local fixture server. Any other value is ignored.
package linear
