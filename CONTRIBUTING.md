# Contributing

Development, tests, and CI are documented in [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

A plugin is an executable named `weeklog-source-<name>`. The contract is [docs/PLUGIN_PROTOCOL.md](docs/PLUGIN_PROTOCOL.md). How to install one is [docs/PLUGINS.md](docs/PLUGINS.md). [examples/weeklog-source-todoist](examples/weeklog-source-todoist/README.md) is a short Python program that speaks protocol v1. It is an example, not a supported source.

## Pull requests

Open one pull request per change, against `main`. Keep the change focused. CI must pass on Ubuntu and macOS.

Do not tag a release or publish binaries. The maintainer merges every pull request and cuts releases.
