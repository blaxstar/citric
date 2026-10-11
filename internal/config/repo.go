package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

// RepoBuildConfig is the per-repo .citric.toml read from the repo root.
// It maps global tool names to repo-specific commands.
type RepoBuildConfig struct {
	Tools []RepoTool `toml:"tool"`
}

// RepoTool is one [[tool]] entry in a repo's .citric.toml.
type RepoTool struct {
	Name    string            `toml:"name"`
	Command string            `toml:"command"`
	Timeout string            `toml:"timeout"` // overrides global timeout when set
	Env     map[string]string `toml:"env"`
	// Shell runs Command: sh, bash, pwsh, powershell or cmd. Default: sh, or
	// PowerShell on Windows.
	Shell string `toml:"shell"`
	// RunsOn lists labels the executor must have, e.g. ["windows"] or
	// ["linux", "gpu"]. Every node has its os and arch as labels.
	RunsOn []string `toml:"runs_on"`
}

// ToolMap returns a name → RepoTool index for fast lookup.
func (c *RepoBuildConfig) ToolMap() map[string]RepoTool {
	m := make(map[string]RepoTool, len(c.Tools))
	for _, t := range c.Tools {
		m[t.Name] = t
	}
	return m
}

// LoadRepoBuildConfig reads a .citric.toml from path.
// Returns an empty config if the file does not exist (not an error).
func LoadRepoBuildConfig(path string) (*RepoBuildConfig, error) {
	var cfg RepoBuildConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return &RepoBuildConfig{}, nil
		}
		return nil, err
	}
	return &cfg, nil
}

// ParseRepoBuildConfig parses a .citric.toml already held in memory. The runner
// uses this to read a commit's config straight out of the bare repo (git show),
// so it can tell whether a tool is even defined before doing any worktree work.
// Empty input yields an empty config, matching LoadRepoBuildConfig's behaviour
// for a missing file.
func ParseRepoBuildConfig(data []byte) (*RepoBuildConfig, error) {
	var cfg RepoBuildConfig
	if len(data) == 0 {
		return &cfg, nil
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
