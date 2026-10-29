//go:build unix

package cli

import (
	"os"
	"syscall"
)

func openSecretsFile(path string) (*os.File, error) {
	// Nonblocking open lets descriptor validation reject FIFOs without waiting.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

func privateSecretsMode(mode os.FileMode) bool {
	return mode.Perm()&0077 == 0
}
