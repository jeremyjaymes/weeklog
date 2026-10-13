package codex

import (
	"context"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Source collects Codex session activity. The collector arrives in a later milestone.
type Source struct {
	source.Unimplemented
}

// New returns the built-in codex source.
func New() *Source {
	return &Source{Unimplemented: source.Unimplemented{Name: "codex"}}
}

// Describe returns the codex config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     "codex",
		Version:  source.BuiltinVersion,
		Config:   map[string]source.Field{},
	}, nil
}
