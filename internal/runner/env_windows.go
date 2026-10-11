//go:build windows

package runner

import "os"

// platformBuildEnv is the OS-specific part of a build's clean environment.
// Windows programs (PowerShell, git, most toolchains) fail without a handful
// of system variables, so those are passed through from the service; the
// user-profile variables point at the temp dir, matching HOME=/tmp on Unix.
func platformBuildEnv() []string {
	tmp := os.TempDir()
	env := []string{
		"TEMP=" + tmp, "TMP=" + tmp,
		"USERPROFILE=" + tmp, "HOME=" + tmp,
		"APPDATA=" + tmp, "LOCALAPPDATA=" + tmp,
	}
	for _, k := range []string{
		"SystemRoot", "SystemDrive", "windir", "ComSpec", "PATHEXT",
		"ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432",
		"CommonProgramFiles", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE",
		"OS", "PSModulePath",
	} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}
