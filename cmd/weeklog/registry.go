package main

import (
	"github.com/jeremyjaymes/weeklog/internal/source"
	"github.com/jeremyjaymes/weeklog/internal/sources/claudecode"
	"github.com/jeremyjaymes/weeklog/internal/sources/codex"
	"github.com/jeremyjaymes/weeklog/internal/sources/git"
	"github.com/jeremyjaymes/weeklog/internal/sources/github"
	"github.com/jeremyjaymes/weeklog/internal/sources/linear"
)

// builtinSources are the collectors compiled into this binary, in list order.
func builtinSources() []struct {
	name   string
	source source.Source
} {
	return []struct {
		name   string
		source source.Source
	}{
		{name: "git", source: git.New()},
		{name: "claude-code", source: claudecode.New()},
		{name: "codex", source: codex.New()},
		{name: "github", source: github.New()},
		{name: "linear", source: linear.New()},
	}
}

func newSourceRegistry() *source.Registry {
	reg := source.NewRegistry()
	for _, builtin := range builtinSources() {
		if err := reg.Register(builtin.name, builtin.source); err != nil {
			panic(err)
		}
	}
	return reg
}

func builtinByName(name string) (source.Source, bool) {
	for _, builtin := range builtinSources() {
		if builtin.name == name {
			return builtin.source, true
		}
	}
	return nil, false
}
