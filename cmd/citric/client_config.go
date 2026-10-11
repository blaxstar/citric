package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/blaxstar/citric/internal/brand"
)

type ClientConfig struct {
	Server   string `toml:"server"`
	Username string `toml:"username"`
	Token    string `toml:"token"`
}

// RepoPublishConfig holds the open-source mirror settings read from the repo's
// .citric.toml. Only the publish fields are decoded here; the build-tool entries in
// the same file are the server's concern (internal/config.RepoBuildConfig) and are
// ignored by toml's decoder as unknown fields.
type RepoPublishConfig struct {
	PublicRemote string `toml:"public_remote"`
	PublicBranch string `toml:"public_branch"`
	// PublishExclude lists more paths (files or directories) to drop from
	// every published tree, e.g. internal design docs.
	PublishExclude []string `toml:"publish_exclude"`
	// PublishBase starts the public history fresh: every commit up to and
	// including this one is published as a single initial commit, and only
	// later commits are replayed individually.
	PublishBase string `toml:"publish_base"`
	// PublishAuthor ("Name <email>") replaces the author and committer of
	// every published commit.
	PublishAuthor string `toml:"publish_author"`
	// PublishForbid is a regular expression that must not match anything
	// published: file contents, commit messages or identities. Publishing
	// stops with the matches listed if it does.
	PublishForbid string `toml:"publish_forbid"`
}

// loadRepoPublishConfig reads publish settings from <repo-root>/.citric.toml. It
// returns a zero-value config (no error) when there is no .citric.toml, so callers
// can give their own "not configured" message.
func loadRepoPublishConfig() (*RepoPublishConfig, error) {
	root, err := gitOut("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository: %w", err)
	}
	var cfg RepoPublishConfig
	if _, err := toml.DecodeFile(brand.RepoConfigPath(root), &cfg); err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func defaultClientConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	path := filepath.Join(dir, brand.Name, "config.toml")
	if _, err := os.Stat(path); err != nil {
		// Pre-rename installs keep their login in the crit directory.
		legacy := filepath.Join(dir, brand.LegacyName, "config.toml")
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return path
}

func loadClientConfig() (*ClientConfig, error) {
	path := defaultClientConfigPath()
	var cfg ClientConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return &ClientConfig{}, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func saveClientConfig(cfg *ClientConfig) error {
	path := defaultClientConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(cfg)
}
