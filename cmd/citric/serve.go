package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/api"
	"github.com/blaxstar/citric/internal/brand"
	"github.com/blaxstar/citric/internal/config"
	critgit "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/keycloak"
	"github.com/blaxstar/citric/internal/store"
)

var cmdServe = &cobra.Command{
	Use:   "serve",
	Short: "Start the Citric server",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		db, err := store.Open(brand.DataFile(cfg.Server.DataDir, ".db"))
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()

		if err := bootstrapFirstAdmin(cfg, db); err != nil {
			return err
		}

		// Config-file/env values for these win over the admin UI: an operator
		// who pins them in deployment config expects that pin to hold.
		for key, val := range map[string]string{
			"public_url":      cfg.Server.PublicURL,
			"site_title":      cfg.UI.Title,
			"default_palette": cfg.UI.Theme,
		} {
			if val == "" {
				continue
			}
			if err := db.SetSetting(key, val); err != nil {
				return fmt.Errorf("seed %s setting: %w", key, err)
			}
		}

		if cfg.Keycloak.SyncEnabled() {
			interval := 30 * time.Minute
			if cfg.Keycloak.SyncInterval != "" {
				if d, err := time.ParseDuration(cfg.Keycloak.SyncInterval); err == nil && d > 0 {
					interval = d
				}
			}
			go keycloak.RunSync(db, cfg.Keycloak, interval)
		}

		reinstallHooks(cfg)

		srv := api.NewServer(cfg, db)
		addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		log.Printf("citric listening on %s", addr)
		return http.ListenAndServe(addr, srv)
	},
}

// reinstallHooks rewrites citric's git hooks for every bare repo at startup so
// existing repos pick up hook changes shipped in a new binary (best-effort).
func reinstallHooks(cfg *config.Config) {
	serverURL := fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	token, err := critgit.HookToken(cfg.Server.DataDir)
	if err != nil {
		log.Printf("[hooks] hook token: %v — pushes won't open CLs until this is fixed", err)
		return
	}
	entries, err := os.ReadDir(cfg.Server.ReposDir)
	if err != nil {
		return
	}
	hooks := map[string]string{
		"pre-receive":  critgit.PreReceiveHook,
		"post-receive": critgit.PostReceiveHook(serverURL, token),
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".git") {
			continue
		}
		hooksDir := filepath.Join(cfg.Server.ReposDir, e.Name(), "hooks")
		for name, content := range hooks {
			p := filepath.Join(hooksDir, name)
			if err := os.WriteFile(p, []byte(content), critgit.HookFileMode); err != nil {
				log.Printf("[hooks] %s/%s: %v", e.Name(), name, err)
			}
			_ = os.Chmod(p, critgit.HookFileMode) // WriteFile keeps an existing file's mode
		}
	}
}

// cmdRepo groups server-side repository management commands. Day-to-day repo
// creation is done through the admin web UI; these are the CLI equivalents for
// running directly on the server box.
var cmdRepo = &cobra.Command{
	Use:   "repo",
	Short: "Server-side repository management (run on the citric server)",
}

var cmdRepoInit = &cobra.Command{
	Use:   "init <repo>",
	Short: "Initialize a bare git repo and install citric hooks (server-side)",
	Long: `Creates a bare git repository and installs citric's pre-receive and
post-receive hooks. If <repo> is a relative name (no slashes), it is resolved
to repos_dir/<repo>.git using the config file. Use an absolute path to target
any directory without needing the config file.

This runs on the citric server. To set up a *local* repo to push to citric, use
'citric init' instead.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serverURL, _ := cmd.Flags().GetString("server")

		repoPath := args[0]
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		hookToken, err := critgit.HookToken(cfg.Server.DataDir)
		if err != nil {
			return fmt.Errorf("hook token: %w", err)
		}
		if !strings.Contains(repoPath, "/") {
			name := repoPath
			if !strings.HasSuffix(name, ".git") {
				name += ".git"
			}
			repoPath = filepath.Join(cfg.Server.ReposDir, name)
			if serverURL == "" {
				serverURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
			}
		}
		if serverURL == "" {
			serverURL = "http://localhost:4400"
		}

		// Create bare repo if it doesn't exist yet. --shared=group makes it
		// group-writable + setgid so both the service user and dev group can
		// write objects regardless of who ran this command.
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			out, err := exec.Command("git", "init", "--bare", "--shared=group", repoPath).CombinedOutput()
			if err != nil {
				return fmt.Errorf("git init --bare: %w\n%s", err, out)
			}
			fmt.Printf("initialized bare repository: %s\n", repoPath)
		}

		// Allow push via git smart HTTP (git-http-backend checks this config key)
		if out, err := exec.Command("git", "config", "-f", filepath.Join(repoPath, "config"),
			"http.receivepack", "true").CombinedOutput(); err != nil {
			return fmt.Errorf("set http.receivepack: %w\n%s", err, out)
		}

		// Write hooks
		hooksDir := filepath.Join(repoPath, "hooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			return fmt.Errorf("create hooks dir: %w", err)
		}
		hooks := map[string]string{
			"pre-receive":  critgit.PreReceiveHook,
			"post-receive": critgit.PostReceiveHook(serverURL, hookToken),
		}
		for name, content := range hooks {
			p := filepath.Join(hooksDir, name)
			if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
				return fmt.Errorf("write %s: %w", name, err)
			}
			fmt.Printf("installed %s\n", name)
		}

		repoName := strings.TrimSuffix(filepath.Base(repoPath), ".git")
		fmt.Printf("\nready. push with:\n  git remote add origin <citric-url>/git/%s.git\n  citric push\n", repoName)
		return nil
	},
}

func init() {
	cmdRepoInit.Flags().String("server", "", "citric server URL for hooks (default: derived from config)")
	cmdRepo.AddCommand(cmdRepoInit)
}
