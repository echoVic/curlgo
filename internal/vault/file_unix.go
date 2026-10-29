//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package vault

import (
	"os"
	"syscall"
)

func openReadFile(path string) (*os.File, error) {
	// Nonblocking prevents a swapped FIFO from blocking before descriptor checks.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
