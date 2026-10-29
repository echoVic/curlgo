//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package vault

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestPrivateOpenerRejectsSymlinkAndFIFO(t *testing.T) {
	path := testPath(t)
	writeTestFile(t, path, validEnvelopeJSON(t))
	f, err := openPrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	fifo := path + ".fifo"
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{link, fifo} {
		if f, err := openPrivateFile(unsafe); !errors.Is(err, ErrFile) {
			if f != nil {
				_ = f.Close()
			}
			t.Fatalf("unsafe descriptor accepted: %v", err)
		}
	}
	if _, err := Open(fifo, &fakeKeyStore{}); !errors.Is(err, ErrFile) {
		t.Fatalf("FIFO vault accepted: %v", err)
	}
}
