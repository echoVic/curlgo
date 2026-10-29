//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package vault

import "os"

func openReadFile(path string) (*os.File, error) {
	// Other platforms retain readEnvelope's Lstat/descriptor identity checks.
	return os.Open(path)
}
