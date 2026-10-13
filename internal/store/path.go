package store

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultPath is the SQLite file weeklog collect uses when --db is omitted.
// XDG_DATA_HOME/weeklog/weeklog.db is used when XDG_DATA_HOME is set.
// Otherwise the path is ~/.local/share/weeklog/weeklog.db, or
// %USERPROFILE%\AppData\Local\weeklog\weeklog.db on Windows.
func DefaultPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dir != "" {
		return filepath.Join(dir, "weeklog", "weeklog.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find data directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Local", "weeklog", "weeklog.db"), nil
	}
	return filepath.Join(home, ".local", "share", "weeklog", "weeklog.db"), nil
}
