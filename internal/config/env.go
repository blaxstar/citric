package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/blaxstar/citric/internal/brand"
)

// applyEnv overlays CITRIC_* environment variables (or their legacy CRIT_*
// spelling) onto cfg. Secrets also accept a *_FILE variant naming a file to
// read the value from, for Docker/Kubernetes secrets.
func applyEnv(cfg *Config) error {
	str := func(key string, dst *string) {
		if v, ok := brand.LookupEnv(key); ok {
			*dst = v
		}
	}
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	num := func(key string, dst *int) {
		if v, ok := brand.LookupEnv(key); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				fail(fmt.Errorf("%s: %q is not a number", brand.EnvName(key), v))
				return
			}
			*dst = n
		}
	}
	flag := func(key string, dst *bool) {
		if v, ok := brand.LookupEnv(key); ok {
			b, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				fail(fmt.Errorf("%s: %q is not true/false", brand.EnvName(key), v))
				return
			}
			*dst = b
		}
	}
	secret := func(key string, dst *string) {
		if path, ok := brand.LookupEnv(key + "_FILE"); ok {
			b, err := os.ReadFile(path)
			if err != nil {
				fail(fmt.Errorf("%s_FILE: %w", brand.EnvName(key), err))
				return
			}
			*dst = strings.TrimSpace(string(b))
			return
		}
		str(key, dst)
	}

	s := &cfg.Server
	str("HOST", &s.Host)
	num("PORT", &s.Port)
	str("PUBLIC_URL", &s.PublicURL)
	str("REPOS_DIR", &s.ReposDir)
	str("DATA_DIR", &s.DataDir)
	str("CACHE_DIR", &s.CacheDir)
	num("MAX_CONCURRENT", &s.MaxConcurrent)
	num("MAX_CONCURRENT_PER_CL", &s.MaxConcurrentPerCL)
	flag("QUEUE_MODE", &s.QueueMode)
	str("BUILD_PATH", &s.BuildPath)
	str("DIST_DIR", &s.DistDir)
	if v, ok := brand.LookupEnv("LOCAL_BUILDS"); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			fail(fmt.Errorf("%s: %q is not true/false", brand.EnvName("LOCAL_BUILDS"), v))
		} else {
			s.LocalBuilds = &b
		}
	}
	if v, ok := brand.LookupEnv("LOCAL_LABELS"); ok {
		s.LocalLabels = SplitList(v)
	}

	secret("JWT_SECRET", &cfg.Auth.JWTSecret)
	str("TOKEN_TTL", &cfg.Auth.TokenTTL)

	k := &cfg.Keycloak
	str("OIDC_ISSUER", &k.Issuer)
	str("OIDC_CLIENT_ID", &k.ClientID)
	secret("OIDC_CLIENT_SECRET", &k.ClientSecret)
	str("OIDC_ADMIN_ROLE", &k.AdminRole)
	str("OIDC_ROLES_CLAIM", &k.RolesClaim)
	str("KEYCLOAK_URL", &k.URL)
	str("KEYCLOAK_REALM", &k.Realm)
	str("KEYCLOAK_ADMIN_URL", &k.InternalURL)
	str("KEYCLOAK_SYNC_INTERVAL", &k.SyncInterval)
	if v, ok := brand.LookupEnv("KEYCLOAK_SYNC"); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			fail(fmt.Errorf("%s: %q is not true/false", brand.EnvName("KEYCLOAK_SYNC"), v))
		} else {
			k.SyncDisabled = !b
		}
	}

	str("SITE_TITLE", &cfg.UI.Title)
	str("THEME", &cfg.UI.Theme)
	return firstErr
}

// SplitList splits a comma-separated list, trimming blanks.
func SplitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
