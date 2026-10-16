// Package actor assigns one actor to a work item from stored evidence.
//
// Attribute reads commit and pull-request text, linker reasons, branches,
// and agent-session sources. It does not invent an actor when that evidence
// is missing or names more than one actor. The rules and their order are
// documented in docs/ATTRIBUTION.md.
package actor
