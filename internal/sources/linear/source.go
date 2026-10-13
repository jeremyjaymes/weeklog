package linear

import (
	"context"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

// Source collects ticket activity. The collector arrives in a later milestone.
type Source struct {
	source.Unimplemented
}

// New returns the built-in linear source.
func New() *Source {
	return &Source{Unimplemented: source.Unimplemented{Name: "linear"}}
}

// Describe returns the linear config schema.
func (s *Source) Describe(ctx context.Context) (source.Manifest, error) {
	if err := ctx.Err(); err != nil {
		return source.Manifest{}, err
	}
	return source.Manifest{
		Protocol: source.ProtocolVersion,
		Name:     "linear",
		Version:  source.BuiltinVersion,
		Config: map[string]source.Field{
			"token":          {Type: source.TypeSecret, Required: true},
			"ticket_pattern": {Type: source.TypeString},
		},
	}, nil
}
