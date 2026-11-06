package cli

import (
	"runtime"
	"testing"
)

// posixPermissions reports whether the OS exposes POSIX file permission bits.
// Windows only models the read-only flag and always reports 0666/0777 through
// os.FileMode.Perm, so 0600/0700 assertions are meaningless there.
func posixPermissions() bool {
	return runtime.GOOS != "windows"
}

// setTempDir points os.TempDir at dir on every supported OS. POSIX chooses the
// temporary directory from TMPDIR, while Windows reads TMP and then TEMP.
func setTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, dir)
	}
}

// measuredMilliseconds reports whether a duration expected to be positive was
// measured as positive. The Windows acceptance VM can round a phase that is
// shorter than its timer resolution to zero; macOS/Linux keep the strict
// positive check.
func measuredMilliseconds(ms float64) bool {
	if runtime.GOOS == "windows" {
		return ms >= 0
	}
	return ms > 0
}
