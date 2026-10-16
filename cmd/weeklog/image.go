package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jeremyjaymes/weeklog/internal/config"
)

// errFreezeNotInstalled is returned when --image cannot find the freeze command.
// charmbracelet/freeze keeps the screenshot renderer in package main, so weeklog
// cannot import it. Current freeze releases also require Go 1.24, and this
// module stays on Go 1.23. The freeze binary is the supported interface.
var errFreezeNotInstalled = errors.New("freeze is not installed: weeklog week --image needs the freeze command on PATH to write a PNG or SVG (https://github.com/charmbracelet/freeze). Install it with `go install github.com/charmbracelet/freeze@latest` or `brew install charmbracelet/tap/freeze`")

// recapImagePath expands path and requires a PNG or SVG extension.
// "-" is rejected: an image has to be a file.
func recapImagePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return "", errors.New("image path must be a .png or .svg file")
	}
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return "", fmt.Errorf("image path: %w", err)
	}
	switch strings.ToLower(filepath.Ext(expanded)) {
	case ".png", ".svg":
		return expanded, nil
	default:
		return "", fmt.Errorf("image path %s must end in .png or .svg", expanded)
	}
}

func requireFreeze() error {
	if _, err := exec.LookPath("freeze"); err != nil {
		return errFreezeNotInstalled
	}
	return nil
}

// writeRecapImage renders body, the colored recap, through the freeze command.
// freeze's base config frames the ANSI text. The file is overwritten.
func writeRecapImage(ctx context.Context, path, body string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	bin, err := exec.LookPath("freeze")
	if err != nil {
		return errFreezeNotInstalled
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("image directory: %w", err)
		}
	}
	cmd := exec.CommandContext(ctx, bin, "--config", "base", "--language", "ansi", "--output", path)
	cmd.Stdin = strings.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := freezeOutput(stdout.String(), stderr.String())
		if detail == "" {
			return fmt.Errorf("freeze: %w", err)
		}
		return fmt.Errorf("freeze: %w: %s", err, detail)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("freeze did not write %s", path)
	}
	if info.IsDir() || info.Size() == 0 {
		return fmt.Errorf("freeze wrote an empty file: %s", path)
	}
	return nil
}

func freezeOutput(stdout, stderr string) string {
	text := strings.TrimSpace(stderr)
	if text == "" {
		text = strings.TrimSpace(stdout)
	}
	text = strings.Join(strings.Fields(text), " ")
	const max = 400
	if len(text) > max {
		text = text[:max]
	}
	return text
}
