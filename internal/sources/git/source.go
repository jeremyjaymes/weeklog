package git

import (
	"context"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Source collects commit activity. The collector arrives in a later milestone.
type Source struct {
	source.Unimplemented
}

// New returns the built-in git source.
func New() *Source {
	return &Source{Unimplemented: source.Unimplemented{Name: "git"}}
}

// Describe returns the git config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     "git",
		Version:  source.BuiltinVersion,
		Config: map[string]source.Field{
			"roots":   {Type: source.TypeStrings, Required: true},
			"authors": {Type: source.TypeStrings},
		},
	}, nil
}
