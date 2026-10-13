// Package redact removes secrets from weeklog event fields before they are stored.
//
// Redaction is always on. Built-in rules cover known token shapes, assignment
// values, private keys, and high-entropy tokens. Exact values resolved from
// weeklog's own config are removed wherever they appear. Extra patterns and
// an allow list come from the config [redaction] table.
package redact
