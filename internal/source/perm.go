package source

import (
	"os"
	"runtime"
)

func directorySearchable(info os.FileInfo) bool {
	if privileged() || runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o100 != 0
}
