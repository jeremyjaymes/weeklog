package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	configAppDir   = "weeklog"
	configFileName = "config.toml"
)

// DefaultPath returns the config file path from XDG_CONFIG_HOME when that
// variable is set, and otherwise from the OS user config directory.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, configAppDir, configFileName), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config directory: %w", err)
	}
	return filepath.Join(dir, configAppDir, configFileName), nil
}

// ResolveConfigPath returns flag when it is set and the default path otherwise.
// A leading ~ in flag is expanded to the user's home directory.
func ResolveConfigPath(flag string) (string, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return DefaultPath()
	}
	path, err := cleanPath(flag)
	if err != nil {
		return "", fmt.Errorf("config path: %w", err)
	}
	return path, nil
}

// cleanPath expands a leading ~ and cleans the path. An empty path stays empty.
func cleanPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	expanded, err := expandUser(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(expanded), nil
}

// expandUser expands a leading ~ to the user's home directory.
// ~user paths are rejected.
func expandUser(path string) (string, error) {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand ~: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:]), nil
	}
	return "", fmt.Errorf("expand %q: ~user paths are not supported", path)
}
