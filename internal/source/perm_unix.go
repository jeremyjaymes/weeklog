//go:build unix

package source

import "os"

func privileged() bool {
	return os.Geteuid() == 0
}
