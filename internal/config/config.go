package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server Server `toml:"server"`
	Auth   Auth   `toml:"auth"`
	// OIDC is single sign-on against any OpenID Connect provider. [keycloak]
	// is its pre-rename spelling and is still read; keys set under [oidc] win.
	// After Load, the merged result lives in Keycloak.
	OIDC       Keycloak    `toml:"oidc"`
	Keycloak   Keycloak    `toml:"keycloak"`
	UI         UI          `toml:"ui"`
	BuildTools []BuildTool `toml:"build_tool"`
}

// UI holds presentation settings that seed the admin-editable DB settings.
type UI struct {
	// Title is the instance name shown in the web UI (the site_title setting).
	Title string `toml:"title"`
	// Theme is the default color palette id (the default_palette setting),
	// e.g. "navel"; people can still pick their own in Appearance.
	Theme string `toml:"theme"`
}

// Keycloak configures single sign-on. Despite the name it works with any
// OIDC provider: set Issuer (or URL + Realm for Keycloak), ClientID and
// ClientSecret. URL + Realm additionally enable Keycloak user/role sync.
type Keycloak struct {
	// Issuer is the provider's OIDC issuer URL; discovery is read from
	// <Issuer>/.well-known/openid-configuration. Derived from URL + Realm
	// when unset.
	Issuer string `toml:"issuer"`
	// RolesClaim names an extra token claim (dot path, e.g. "groups" or
	// "app.roles") to read role names from, beside the standard ones.
	RolesClaim string `toml:"roles_claim"`
	// SyncDisabled turns off Keycloak user/role sync even when URL + Realm
	// are set.
	SyncDisabled bool `toml:"sync_disabled"`

	URL string `toml:"url"`
	// InternalURL is used for server-side admin API calls (sync, role fetches).
	// Set this to the container's internal address (e.g. http://keycloak:8080)
	// when the public URL has the /admin/ paths blocked by a reverse proxy.
	// If unset, URL is used for all calls.
	InternalURL  string `toml:"internal_url"`
	Realm        string `toml:"realm"`
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	SyncInterval string `toml:"sync_interval"`
	// AdminRole is the Keycloak realm role that grants citric admin. Users with
	// this role on their SSO token become admins; everyone else is a reviewer.
	AdminRole string `toml:"admin_role"`
}

// SyncEnabled reports whether Keycloak user/role sync should run: it needs
// the Keycloak admin API (URL + Realm) and a client to call it with.
func (k Keycloak) SyncEnabled() bool {
	return !k.SyncDisabled && k.ClientID != "" && k.URL != "" && k.Realm != ""
}

// SSOEnabled reports whether web single sign-on is configured.
func (k Keycloak) SSOEnabled() bool {
	return k.ClientID != "" && k.Issuer != ""
}

// AdminURL returns InternalURL if set, otherwise URL. Use this for server-side
// Keycloak admin API calls that should bypass the public reverse proxy.
func (k Keycloak) AdminURL() string {
	if k.InternalURL != "" {
		return k.InternalURL
	}
	return k.URL
}

type Server struct {
	// PublicURL is the externally reachable base URL (e.g.
	// https://citric.example.com), used for clone URLs and install.sh. It seeds
	// the public_url setting; when empty, the request's host is used.
	PublicURL string `toml:"public_url"`

	Host          string `toml:"host"`
	Port          int    `toml:"port"`
	ReposDir      string `toml:"repos_dir"`
	DataDir       string `toml:"data_dir"`
	MaxConcurrent int    `toml:"max_concurrent"`
	// MaxConcurrentPerCL caps how many of a single CL's own tools may run at
	// once, independent of (and in addition to) MaxConcurrent. Without this,
	// every tool in one push (lint/test/integration/...) starts at once and
	// can saturate the box on its own even when nothing else is building —
	// MaxConcurrent alone doesn't help there since it's shared across every
	// CL on the server, not scoped to one. See critcache.md.
	MaxConcurrentPerCL int `toml:"max_concurrent_per_cl"`
	// QueueMode, when true, serializes builds one CL at a time: a CL's tools
	// only start once every tool from the previously-running CL has finished,
	// completely ignoring MaxConcurrent (MaxConcurrentPerCL still applies
	// among the one active CL's own tools). For a box that can't reliably
	// finish even MaxConcurrent=1's worth of *cross-CL* contention within a
	// tool's timeout — see critcache.md's follow-up incident — this trades
	// server-wide throughput for every individual build actually finishing.
	QueueMode bool   `toml:"queue_mode"`
	BuildPath string `toml:"build_path"` // PATH passed to build commands; defaults to a safe minimal value
	// CacheDir holds the shared, persistent build caches (cargo, sccache,
	// GOCACHE, GOMODCACHE). It must live OUTSIDE any repo working tree:
	// citric's own DataDir sits inside the citric repo, and a module cache under
	// it makes `go vet ./...` and `gofmt -l .` walk into vendored source and
	// fail on it. Defaults to DataDir/cache only for backward compatibility.
	CacheDir string `toml:"cache_dir"`
	// DistDir is a second place /dl looks for client binaries after
	// <data_dir>/dist; packages and the container image ship them here.
	// Default: /usr/share/citric/dist.
	DistDir string `toml:"dist_dir"`
	// LocalBuilds lets the server run build tools itself ("node zero").
	// Default true; turn it off to send every build to nodes, e.g. so CLs
	// from untrusted contributors never run on the server.
	LocalBuilds *bool `toml:"local_builds"`
	// LocalLabels are extra runs_on labels the server offers beside its os
	// and arch.
	LocalLabels []string `toml:"local_labels"`
}

// RunsLocalBuilds reports whether the server runs builds itself.
func (s Server) RunsLocalBuilds() bool {
	return s.LocalBuilds == nil || *s.LocalBuilds
}

type Auth struct {
	JWTSecret string `toml:"jwt_secret"`
	TokenTTL  string `toml:"token_ttl"`
}

type BuildTool struct {
	Name      string            `toml:"name"`
	Command   string            `toml:"command"`
	Timeout   string            `toml:"timeout"`
	Required  bool              `toml:"required"`
	DependsOn []string          `toml:"depends_on"`
	Env       map[string]string `toml:"env"`
	// Shell is the default shell for this tool's commands; a repo's
	// .citric.toml can override it per tool.
	Shell string `toml:"shell"`
	// RunsOn is the default executor label requirement; a repo's
	// .citric.toml can override it per tool.
	RunsOn []string `toml:"runs_on"`
}

// Load reads the server config. path may be empty or name a file that does
// not exist, in which case citric starts from defaults plus the environment:
// every setting has a CITRIC_* variable (see applyEnv), so a container needs
// no config file at all. Precedence is env > file > default.
func Load(path string) (*Config, error) {
	var cfg Config
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			if _, err := toml.Decode(string(data), &cfg); err != nil {
				return nil, fmt.Errorf("parse config: %w", err)
			}
		case os.IsNotExist(err):
			// env-only configuration
		default:
			return nil, fmt.Errorf("read config: %w", err)
		}
	}
	cfg.Keycloak = mergeOIDC(cfg.OIDC, cfg.Keycloak)
	cfg.OIDC = Keycloak{}
	if err := applyEnv(&cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	if err := ensureJWTSecret(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// mergeOIDC overlays the [oidc] section onto the legacy [keycloak] one, key by
// key, so either spelling (or a mix during migration) works.
func mergeOIDC(oidc, kc Keycloak) Keycloak {
	pick := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	return Keycloak{
		Issuer:       pick(oidc.Issuer, kc.Issuer),
		RolesClaim:   pick(oidc.RolesClaim, kc.RolesClaim),
		SyncDisabled: oidc.SyncDisabled || kc.SyncDisabled,
		URL:          pick(oidc.URL, kc.URL),
		InternalURL:  pick(oidc.InternalURL, kc.InternalURL),
		Realm:        pick(oidc.Realm, kc.Realm),
		ClientID:     pick(oidc.ClientID, kc.ClientID),
		ClientSecret: pick(oidc.ClientSecret, kc.ClientSecret),
		SyncInterval: pick(oidc.SyncInterval, kc.SyncInterval),
		AdminRole:    pick(oidc.AdminRole, kc.AdminRole),
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 4400
	}
	if cfg.Server.DataDir == "" {
		cfg.Server.DataDir = filepath.Join(defaultStateDir(), "data")
	}
	if cfg.Server.ReposDir == "" {
		cfg.Server.ReposDir = filepath.Join(defaultStateDir(), "repos")
	}
	if cfg.Auth.TokenTTL == "" {
		cfg.Auth.TokenTTL = "720h"
	}
	if cfg.Server.MaxConcurrent == 0 {
		cfg.Server.MaxConcurrent = 4
	}
	if cfg.Server.MaxConcurrentPerCL == 0 {
		cfg.Server.MaxConcurrentPerCL = 2
	}
	if cfg.Server.BuildPath == "" {
		cfg.Server.BuildPath = defaultBuildPath()
	}
	if cfg.Server.CacheDir == "" {
		cfg.Server.CacheDir = filepath.Join(cfg.Server.DataDir, "cache")
	}
	if cfg.Server.DistDir == "" && runtime.GOOS != "windows" {
		cfg.Server.DistDir = "/usr/share/citric/dist"
	}
	if cfg.Keycloak.AdminRole == "" {
		cfg.Keycloak.AdminRole = "citric-admin"
	}
	if len(cfg.BuildTools) == 0 {
		cfg.BuildTools = DefaultBuildTools()
	}
	if cfg.Keycloak.Issuer == "" && cfg.Keycloak.URL != "" && cfg.Keycloak.Realm != "" {
		cfg.Keycloak.Issuer = strings.TrimRight(cfg.Keycloak.URL, "/") + "/realms/" + cfg.Keycloak.Realm
	}
	cfg.Keycloak.Issuer = strings.TrimRight(cfg.Keycloak.Issuer, "/")
}

// DefaultBuildTools is the tool registry used when the config defines none, so
// a fresh install runs CI out of the box: lint gates submission, test and
// integration follow it, and format runs beside it. Repos supply the
// commands in .citric.toml; a tool a repo doesn't define is skipped.
func DefaultBuildTools() []BuildTool {
	return []BuildTool{
		{Name: "lint", Timeout: "5m", Required: true},
		{Name: "test", Timeout: "10m", DependsOn: []string{"lint"}},
		{Name: "integration", Timeout: "30m", DependsOn: []string{"test"}},
		{Name: "format", Timeout: "2m"},
	}
}

// defaultStateDir is where data and repos live when the config doesn't say:
// /var/lib/citric, or %ProgramData%\citric on Windows.
func defaultStateDir() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "citric")
	}
	return "/var/lib/citric"
}

func defaultBuildPath() string {
	if runtime.GOOS == "windows" {
		// Build commands on Windows need the system tools on PATH (pwsh, git,
		// language toolchains); inherit the service's PATH unless configured.
		return os.Getenv("PATH")
	}
	return "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
}

// ensureJWTSecret fills Auth.JWTSecret when neither the file nor the env set
// it: from <data_dir>/jwt_secret, generating that file (0600) on first run so
// a fresh install never ships a guessable signing key.
func ensureJWTSecret(cfg *Config) error {
	if cfg.Auth.JWTSecret != "" {
		return nil
	}
	path := filepath.Join(cfg.Server.DataDir, "jwt_secret")
	if b, err := os.ReadFile(path); err == nil {
		if secret := strings.TrimSpace(string(b)); secret != "" {
			cfg.Auth.JWTSecret = secret
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("generate jwt secret: %w", err)
	}
	secret := hex.EncodeToString(buf)
	if err := os.MkdirAll(cfg.Server.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	cfg.Auth.JWTSecret = secret
	return nil
}
