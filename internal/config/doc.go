// Package config loads weeklog configuration from TOML.
//
// The default path is $XDG_CONFIG_HOME/weeklog/config.toml, or
// $HOME/.config/weeklog/config.toml when XDG_CONFIG_HOME is unset.
// A literal secret is the normal form. env:, cmd:, and file: references
// are optional. Resolved secret values are kept out of check reports and
// formatted output.
//
// Init writes a starter file with mode 0600 after detecting local sources.
// The non-interactive file does not contain secret values. GitHub uses
// cmd:gh auth token. Commented Linear and cursor-cloud blocks use
// token = "" and tell the user to paste a key. A cursor-cloud token is
// one literal value. WriteInitConfig can store pasted tokens as literals
// in that same file. Reports redact those values. No .env file is written.
package config
