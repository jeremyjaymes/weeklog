package claudecode

import (
	"context"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Source collects Claude Code session activity. The collector arrives in a later milestone.
type Source struct {
	source.Unimplemented
}

// New returns the built-in claude-code source.
func New() *Source {
	return &Source{Unimplemented: source.Unimplemented{Name: "claude-code"}}
}

// Describe returns the claude-code config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     "claude-code",
		Version:  source.BuiltinVersion,
		Config:   map[string]source.Field{},
	}, nil
}
