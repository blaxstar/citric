// Package brand holds the product's public name and the legacy names it still
// answers to. citric was released from an internal tool called crit; existing
// installs keep working because every file, env var and command name below
// falls back to its crit-era spelling when the citric one is absent.
package brand

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// Name is the product and binary name.
	Name = "citric"
	// LegacyName is the pre-release name, still accepted as an alias.
	LegacyName = "crit"

	// RepoConfigFile is the per-repo build/publish config at the repo root.
	RepoConfigFile = ".citric.toml"
	// LegacyRepoConfigFile is read when RepoConfigFile is absent.
	LegacyRepoConfigFile = ".crit.toml"

	envPrefix       = "CITRIC_"
	legacyEnvPrefix = "CRIT_"
)

// RepoConfigPath returns the repo config file to use under root: .citric.toml
// if it exists, else .crit.toml if that exists, else .citric.toml (the name a
// new file should get).
func RepoConfigPath(root string) string {
	p := filepath.Join(root, RepoConfigFile)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	legacy := filepath.Join(root, LegacyRepoConfigFile)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return p
}

// RepoConfigFiles lists the repo config names in lookup order, for callers
// that read a commit's tree rather than a working directory.
func RepoConfigFiles() []string {
	return []string{RepoConfigFile, LegacyRepoConfigFile}
}

// Getenv reads CITRIC_<key>, falling back to CRIT_<key>.
func Getenv(key string) string {
	if v, ok := os.LookupEnv(envPrefix + key); ok {
		return v
	}
	return os.Getenv(legacyEnvPrefix + key)
}

// EnvName is the documented variable name for key, e.g. CITRIC_PORT.
func EnvName(key string) string { return envPrefix + key }

// LookupEnv is Getenv that also reports whether either variable was set.
func LookupEnv(key string) (string, bool) {
	if v, ok := os.LookupEnv(envPrefix + key); ok {
		return v, true
	}
	return os.LookupEnv(legacyEnvPrefix + key)
}

// BuildEnv returns KEY=value pairs for a build command, under both the citric
// and the legacy crit prefix so existing build scripts keep working.
func BuildEnv(key, value string) []string {
	return []string{envPrefix + key + "=" + value, legacyEnvPrefix + key + "=" + value}
}

// Invoked returns the name the binary was run as when it is one of the known
// names, so help text and hints match what the user typed; otherwise Name.
func Invoked() string {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if base == LegacyName {
		return LegacyName
	}
	return Name
}

// DataFile returns name-prefixed file path in dir with the given suffix
// (e.g. ".db"), preferring citric<suffix> but returning crit<suffix> when only
// the pre-rename file exists, so an existing install keeps its data.
func DataFile(dir, suffix string) string {
	p := filepath.Join(dir, Name+suffix)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	legacy := filepath.Join(dir, LegacyName+suffix)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return p
}

// IsServiceUser reports whether a git pusher name is the server's own
// identity (current or pre-rename), as opposed to a real user.
func IsServiceUser(name string) bool {
	return name == Name || name == LegacyName
}
