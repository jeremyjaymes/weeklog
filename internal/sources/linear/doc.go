// Package linear collects Linear issues and state history for weeklog.
//
// Collect calls the GraphQL API for issues assigned to or created by the
// authenticated user that were updated in the window, and the state changes
// in that window. The token comes from the source config. ticket_pattern
// selects the ticket keys stored on each event. When it is omitted, the
// built-in ticket shape is used. The source does not keep a cursor. Tests
// replay recorded responses and do not call Linear.
package linear
