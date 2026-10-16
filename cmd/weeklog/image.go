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
	"github.com/jeremyjaymes/weeklog/internal/redact"
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
	if err := prepareImagePath(path); err != nil {
		return err
	}
	// Bind the path to the flag. A separate argv that starts with "-"
	// (for example -week.png) would be parsed as another freeze option.
	cmd := exec.CommandContext(ctx, bin, "--config", "base", "--language", "ansi", "--output="+path)
	cmd.Stdin = strings.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// freeze can echo a slice of the recap. The recap is already rendered
		// from stored text; this pass catches a token shape in that echo.
		detail := redact.Default().Text(freezeOutput(stdout.String(), stderr.String()))
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

// prepareImagePath rejects a directory and replaces a symlink with the
// file freeze is about to create. Opening the path would follow the link
// and truncate whatever it names. The weekly Markdown writer uses rename,
// which replaces the directory entry instead of following it.
func prepareImagePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("image path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("image path: %w", err)
		}
		return nil
	}
	if info.IsDir() {
		return fmt.Errorf("image path is a directory: %s", path)
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
