//go:build !windows

package runner

// platformBuildEnv is the OS-specific part of a build's clean environment.
// Builds get a throwaway HOME so nothing from the service user's real home
// (credentials, caches) leaks into or out of a build.
func platformBuildEnv() []string {
	return []string{"HOME=/tmp", "TMPDIR=/tmp"}
}
