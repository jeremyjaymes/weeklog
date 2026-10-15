// Package config loads weeklog configuration from TOML.
//
// The default path is $XDG_CONFIG_HOME/weeklog/config.toml, or
// $HOME/.config/weeklog/config.toml when XDG_CONFIG_HOME is unset.
// A literal secret is the normal form. env:, cmd:, and file: references
// are optional. Resolved secret values are kept out of check reports and
// formatted output.
//
// Init writes a starter file with mode 0600 after detecting local sources.
// The file does not contain secret values. GitHub uses cmd:gh auth token.
// A commented Linear block uses token = "" and tells the user to paste a key.
package config
