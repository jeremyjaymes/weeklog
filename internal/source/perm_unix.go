//go:build unix

package source

import (
	"os"
	"syscall"
)

func privileged() bool {
	return os.Geteuid() == 0
}

// directoryWritable reports whether the current user can write path.
// The owner write bit is not enough: the caller must be the owner, or the
// group or other write bit must apply to the caller. Root is always writable.
func directoryWritable(path string) bool {
	if privileged() {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return false
	}
	mode := info.Mode().Perm()
	if int(stat.Uid) == os.Geteuid() {
		return mode&0o200 != 0
	}
	if int(stat.Gid) == os.Getegid() {
		return mode&0o020 != 0
	}
	return mode&0o002 != 0
}

// pluginTrustReason reports why a plugin must not be executed.
// A group- or world-writable file or parent directory, or one owned by
// someone other than the current user or root, can be replaced by another
// local user. An empty reason means the plugin may run.
func pluginTrustReason(dir string, info os.FileInfo) string {
	if reason := untrustedReason(info); reason != "" {
		return reason
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return "cannot stat"
	}
	return untrustedReason(dirInfo)
}

func untrustedReason(info os.FileInfo) string {
	if info.Mode()&0o022 != 0 {
		return "group/world-writable"
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return "not owned by you or root"
	}
	if int(stat.Uid) != os.Geteuid() && stat.Uid != 0 {
		return "not owned by you or root"
	}
	return ""
}
