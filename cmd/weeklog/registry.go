package main

import (
	"context"

	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/sources/claudecode"
	"github.com/jeremyjaymes/weeklog/internal/sources/codex"
	"github.com/jeremyjaymes/weeklog/internal/sources/git"
	"github.com/jeremyjaymes/weeklog/internal/sources/github"
	"github.com/jeremyjaymes/weeklog/internal/sources/linear"
)

type builtin struct {
	name string
	src  source.Source
}

// builtins are the collectors compiled into this binary, in list order.
// Each name is the one the source reports from Describe, so the string is
// not duplicated here.
var builtins = mustBuiltins(
	git.New(),
	claudecode.New(),
	codex.New(),
	github.New(),
	linear.New(),
)

func mustBuiltins(srcs ...source.Source) []builtin {
	out := make([]builtin, 0, len(srcs))
	for _, src := range srcs {
		manifest, err := src.Describe(context.Background())
		if err != nil {
			panic(err)
		}
		out = append(out, builtin{name: manifest.Name, src: src})
	}
	return out
}

func builtinSources() []builtin {
	return builtins
}

// newSourceRegistry registers the built-in sources.
// It panics if two built-ins share a name. TestNewSourceRegistry constructs
// the registry so CI catches that.
func newSourceRegistry() *source.Registry {
	reg := source.NewRegistry()
	for _, item := range builtins {
		if err := reg.Register(item.name, item.src); err != nil {
			panic(err)
		}
	}
	return reg
}

func builtinByName(name string) (source.Source, bool) {
	for _, item := range builtins {
		if item.name == name {
			return item.src, true
		}
	}
	return nil, false
}
