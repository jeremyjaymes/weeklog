// Package config loads weeklog configuration from TOML.
//
// The default path is $XDG_CONFIG_HOME/weeklog/config.toml, or
// $HOME/.config/weeklog/config.toml when XDG_CONFIG_HOME is unset.
// Secret fields use env:, cmd:, and file:
// references. Resolved secret values are kept out of check reports and
// formatted output.
//
// Init writes a starter file after detecting local sources. The file uses
// secret references and does not contain secret values.
package config
