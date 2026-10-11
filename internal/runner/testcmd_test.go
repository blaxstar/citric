package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// markerCmd is a build command that creates dir/name, holds it for ms
// milliseconds, then deletes it: in sh on Unix, in PowerShell (the default
// build shell) on Windows. Tests watch the markers to see what runs at once.
func markerCmd(dir, name string, ms int) string {
	f := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		q := strings.ReplaceAll(f, "'", "''")
		return fmt.Sprintf(`New-Item -ItemType File -Force -Path '%s' | Out-Null; Start-Sleep -Milliseconds %d; Remove-Item -Force -Path '%s'`, q, ms, q)
	}
	return fmt.Sprintf(`f=%s; touch "$f"; sleep %g; rm -f "$f"`, f, float64(ms)/1000)
}

// testBuildPath is the build PATH for tests: a minimal Unix one, or the
// test process's own PATH on Windows, where PowerShell and git live in
// install-specific places.
func testBuildPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("PATH")
	}
	return "/usr/bin:/bin"
}
