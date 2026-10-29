//go:build unix

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestSecretsFileUnixDescriptorPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0400, 0600, 0700, 0640, 0604, 0644, 0666} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			path := secretsTestFile(t, []byte(`{"schemaVersion":1,"variables":{}}`))
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			// Permissions apply to the opened target, not the symlink pathname.
			link := filepath.Join(t.TempDir(), "private_marker.link")
			if err := os.Symlink(path, link); err != nil {
				t.Fatal(err)
			}
			_, fail := readSecretsFile(link)
			if (fail != nil) != (mode&0077 != 0) {
				t.Fatal("private descriptor permissions were not enforced")
			}
			if fail != nil {
				diagnosticTestSafe(t, fail.Message)
			}
		})
	}
}

func TestSecretsFileUnixFIFORejectedWithoutReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private_marker.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	input := `{"schemaVersion":1,"request":{"url":"https://example.test"}}`
	exit, value, raw := runTestInvoke(t, strings.NewReader(input), "--request", "-", "--secrets-file", path)
	dryRunTestFailure(t, exit, value, raw, ExitInput, "INVALID_SECRETS")
}
