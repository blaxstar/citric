package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/auth"
	"github.com/blaxstar/citric/internal/brand"
	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

// openServerDB loads the server config and opens its database, for the
// server-side commands that run next to (or instead of) 'citric serve'.
func openServerDB() (*config.Config, *store.DB, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	if err := os.MkdirAll(cfg.Server.DataDir, 0o750); err != nil {
		return nil, nil, fmt.Errorf("create data dir: %w", err)
	}
	db, err := store.Open(brand.DataFile(cfg.Server.DataDir, ".db"))
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	return cfg, db, nil
}

// ensureAdmin creates username as an admin, or promotes an existing user, and
// returns a fresh CLI token for them.
func ensureAdmin(cfg *config.Config, db *store.DB, username, email string) (string, error) {
	existing, err := db.GetUser(username)
	if err != nil {
		return "", err
	}
	if existing == nil {
		if err := db.CreateUser(&model.User{Username: username, Email: email, Role: model.RoleAdmin}); err != nil {
			return "", err
		}
	} else if err := db.SetUserRole(username, model.RoleAdmin); err != nil {
		return "", err
	}
	_ = db.WriteAudit(brand.Name, "admin.bootstrap", username, "")
	return auth.IssueJWT(username, cfg.Auth.JWTSecret, cfg.Auth.TokenTTL)
}

// bootstrapFirstAdmin runs at server start: when CITRIC_ADMIN_USER is set and
// the instance has no admin yet, create that user as admin and log a one-time
// CLI token. It never touches an instance that already has an admin.
func bootstrapFirstAdmin(cfg *config.Config, db *store.DB) error {
	username := brand.Getenv("ADMIN_USER")
	if username == "" {
		return nil
	}
	n, err := db.CountAdmins()
	if err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if n > 0 {
		return nil
	}
	token, err := ensureAdmin(cfg, db, username, brand.Getenv("ADMIN_EMAIL"))
	if err != nil {
		return fmt.Errorf("bootstrap admin %q: %w", username, err)
	}
	log.Printf("created first admin %q. CLI token (shown once): %s", username, token)
	log.Printf("sign in with: %s auth login --server <url> --token <token>", brand.Name)
	return nil
}

var cmdAdmin = &cobra.Command{
	Use:   "admin",
	Short: "Server-side administration (run on the citric server)",
}

var cmdAdminCreate = &cobra.Command{
	Use:   "create <username>",
	Short: "Create an admin (or promote a user) and print a CLI token",
	Long: `Create <username> as an admin, or promote an existing user, and print a
CLI token for them. Run it on the server, with the same --config (or env) as
'citric serve'. This is how the first admin gets in when no SSO is configured,
and a break-glass path when SSO is down.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		email, _ := cmd.Flags().GetString("email")
		cfg, db, err := openServerDB()
		if err != nil {
			return err
		}
		defer db.Close()
		token, err := ensureAdmin(cfg, db, args[0], email)
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s is an admin\n\nCLI token:\n%s\n\nnext: %s auth login --server <url> --token <token>\n",
			args[0], token, brand.Name)
		return nil
	},
}

var cmdHealthcheck = &cobra.Command{
	Use:   "healthcheck",
	Short: "Exit 0 if the local citric server is healthy (for container health checks)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Server.Port))
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New("unhealthy: " + resp.Status)
		}
		return nil
	},
}

func init() {
	cmdAdminCreate.Flags().String("email", "", "email address for a newly created user")
	cmdAdmin.AddCommand(cmdAdminCreate)
}
