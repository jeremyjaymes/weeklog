package github

import (
	"context"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Source collects pull request and issue activity. The collector arrives in a later milestone.
type Source struct {
	source.Unimplemented
}

// New returns the built-in github source.
func New() *Source {
	return &Source{Unimplemented: source.Unimplemented{Name: "github"}}
}

// Describe returns the github config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     "github",
		Version:  source.BuiltinVersion,
		Config: map[string]source.Field{
			"token": {Type: source.TypeSecret},
		},
	}, nil
}
