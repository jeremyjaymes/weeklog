package config

import (
	"context"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/sources/claudecode"
	"github.com/jeremyjaymes/weeklog/internal/sources/codex"
	"github.com/jeremyjaymes/weeklog/internal/sources/cursorcloud"
	"github.com/jeremyjaymes/weeklog/internal/sources/git"
	"github.com/jeremyjaymes/weeklog/internal/sources/github"
	"github.com/jeremyjaymes/weeklog/internal/sources/linear"
)

func TestBuiltinManifestMatchesConfigSchema(t *testing.T) {
	sources := []source.Source{
		git.New(),
		claudecode.New(),
		codex.New(),
		github.New(),
		linear.New(),
		cursorcloud.New(),
	}
	for _, src := range sources {
		manifest, err := src.Describe(context.Background())
		if err != nil {
			t.Fatalf("Describe: %v", err)
		}
		canonical, schema, exact, found := canonicalSchema(manifest.Name)
		if !found || !exact || canonical != manifest.Name {
			t.Fatalf("schema for %s: found=%v exact=%v canonical=%s", manifest.Name, found, exact, canonical)
		}
		if len(schema.Fields) != len(manifest.Config) {
			t.Fatalf("%s fields: schema %d, manifest %d", manifest.Name, len(schema.Fields), len(manifest.Config))
		}
		for name, spec := range schema.Fields {
			field, ok := manifest.Config[name]
			if !ok {
				t.Fatalf("%s manifest missing %s", manifest.Name, name)
			}
			if spec.Required != field.Required {
				t.Fatalf("%s %s required: schema %v, manifest %v", manifest.Name, name, spec.Required, field.Required)
			}
			var want string
			switch spec.Kind {
			case fieldString:
				want = source.TypeString
			case fieldStrings:
				want = source.TypeStrings
			case fieldSecret:
				want = source.TypeSecret
			default:
				t.Fatalf("%s %s has unknown schema kind", manifest.Name, name)
			}
			if field.Type != want {
				t.Fatalf("%s %s type = %s, want %s", manifest.Name, name, field.Type, want)
			}
		}
	}
}
