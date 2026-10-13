// Package config loads weeklog configuration from TOML.
//
// The default path is $XDG_CONFIG_HOME/weeklog/config.toml, or the OS user
// config directory (on Linux and macOS, ~/.config/weeklog/config.toml) when
// XDG_CONFIG_HOME is unset. Secret fields use env:, cmd:, and file:
// references. Resolved secret values are kept out of check reports and
// formatted output.
package config
