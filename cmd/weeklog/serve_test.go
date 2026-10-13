package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/jeremyjaymes/weeklog/internal/source"
)

func TestMain(m *testing.M) {
	if name := os.Getenv("WEEKLOG_SERVE_BUILTIN"); name != "" {
		src, ok := builtinByName(name)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown builtin\n")
			os.Exit(1)
		}
		os.Exit(source.Serve(src, os.Args[1:]))
	}
	os.Exit(m.Run())
}
