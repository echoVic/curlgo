//go:build !unix

package cli

import "os"

func openSecretsFile(path string) (*os.File, error) {
	return os.Open(path)
}

// POSIX permission bits do not describe Windows ACLs.
func privateSecretsMode(mode os.FileMode) bool {
	return true
}
