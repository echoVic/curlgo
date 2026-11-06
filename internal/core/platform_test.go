package core

import (
	"runtime"
	"time"
)

// posixPermissions reports whether the OS exposes POSIX file permission bits.
// Windows only models the read-only flag and always reports 0666/0777 through
// os.FileMode.Perm, so 0600/0700 assertions are meaningless there.
func posixPermissions() bool {
	return runtime.GOOS != "windows"
}

// observedTiming reports whether a traced phase that is expected to be positive
// was measured as positive. The Windows acceptance VM can round a phase that is
// shorter than its timer resolution to zero, so zero is tolerated there only;
// macOS/Linux keep the strict positive assertion.
func observedTiming(duration time.Duration) bool {
	if runtime.GOOS == "windows" {
		return duration >= 0
	}
	return duration > 0
}
