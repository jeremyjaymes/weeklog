//go:build windows

package source

import "os"

func privileged() bool {
	return false
}

// directoryWritable reports whether the plugin directory can be written.
// Windows keeps the previous behavior and does not apply the Unix
// owner/group/other checks.
func directoryWritable(string) bool {
	return true
}

// pluginTrustReason reports why a plugin must not be executed.
// Windows does not reject group-writable or foreign-owned plugins; discovery
// only requires the weeklog-source-<name> filename.
func pluginTrustReason(string, os.FileInfo) string {
	return ""
}
