# Plugins

A plugin is an executable named `weeklog-source-<name>`. Built-in sources are compiled into the binary. Put the executable on `PATH` or in the plugin directory (`$XDG_DATA_HOME/weeklog/plugins`, or `~/.local/share/weeklog/plugins`).

The contract is [PLUGIN_PROTOCOL.md](PLUGIN_PROTOCOL.md). That document is the protocol: discovery order, `describe` and `collect`, the event fields, exit codes, and the environment the process receives. This page does not restate it.

[examples/weeklog-source-todoist](../examples/weeklog-source-todoist/README.md) is a short Python program that speaks protocol v1. It is an example, not a supported source. Copy `weeklog-source-todoist` onto `PATH` and configure a `todoist` source with a `token`.

`weeklog doctor` checks paths, permissions, and that each plugin speaks protocol v1, and checks that it answers `describe`. It does not print or resolve secrets. A `[[source]]` table with `type` set to the plugin name is configured the same way as a built-in source.

```bash
weeklog sources
weeklog doctor
```

`sources` lists built-in collectors and plugins after a protocol `describe` check.
