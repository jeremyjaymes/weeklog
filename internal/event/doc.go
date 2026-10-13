// Package event defines the shared weeklog event schema.
//
// Plugins map native activity into an Event. The dedupe key is (Source, SourceID).
// Unknown Kind values are valid and are stored as given so later rendering can
// group them under "Other". Prepare truncates Summary to MaxSummaryRunes.
package event
