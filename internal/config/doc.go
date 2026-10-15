// Package config loads weeklog configuration from TOML.
//
// The default path is $XDG_CONFIG_HOME/weeklog/config.toml, or
// $HOME/.config/weeklog/config.toml when XDG_CONFIG_HOME is unset.
// A .env file in that directory supplies env: references when the
// variable is unset in the process environment. Secret fields use env:,
// cmd:, and file: references. Resolved secret values, including every
// non-empty .env value, are kept out of check reports and formatted output.
//
// Init writes a starter file after detecting local sources, and a mode 600
// .env with commented placeholders. The files use secret references and do
// not contain secret values.
package config
