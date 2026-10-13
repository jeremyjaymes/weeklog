// Package source defines the weeklog source plugin interface and protocol v1.
//
// A Source collects activity. Built-in sources implement Source in process.
// An external plugin is an executable named weeklog-source-<name> on PATH or
// in the weeklog plugin directory. Process implements Source by running that
// executable. Registry discovers both and checks describe output.
//
// Secrets are passed only inside the collect request on stdin. They are not
// placed in argv. The subprocess protocol is documented in
// docs/PLUGIN_PROTOCOL.md.
package source
