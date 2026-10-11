package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "citric.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadEnvOnly(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CITRIC_DATA_DIR", data)
	t.Setenv("CITRIC_PORT", "5500")
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 5500 || cfg.Server.DataDir != data {
		t.Errorf("env not applied: port=%d data=%s", cfg.Server.Port, cfg.Server.DataDir)
	}
	if cfg.Auth.JWTSecret == "" {
		t.Fatal("jwt secret not generated")
	}
	b, err := os.ReadFile(filepath.Join(data, "jwt_secret"))
	if err != nil {
		t.Fatalf("jwt secret not persisted: %v", err)
	}
	again, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if again.Auth.JWTSecret != cfg.Auth.JWTSecret || len(b) == 0 {
		t.Error("second start must reuse the persisted secret, not mint a new one")
	}
}

func TestEnvBeatsFileAndLegacyEnvWorks(t *testing.T) {
	p := writeConfig(t, "[server]\nport = 4400\ndata_dir = \""+filepath.ToSlash(t.TempDir())+"\"\n[auth]\njwt_secret = \"from-file\"\n")
	t.Setenv("CRIT_PORT", "6600")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 6600 {
		t.Errorf("legacy CRIT_PORT should override the file: got %d", cfg.Server.Port)
	}
	t.Setenv("CITRIC_PORT", "7700")
	if cfg, _ = Load(p); cfg.Server.Port != 7700 {
		t.Errorf("CITRIC_PORT should win over CRIT_PORT: got %d", cfg.Server.Port)
	}
}

func TestSecretFromFile(t *testing.T) {
	sec := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(sec, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CITRIC_DATA_DIR", t.TempDir())
	t.Setenv("CITRIC_OIDC_CLIENT_SECRET_FILE", sec)
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keycloak.ClientSecret != "s3cret" {
		t.Errorf("got %q", cfg.Keycloak.ClientSecret)
	}
}

func TestBadNumberIsAnError(t *testing.T) {
	t.Setenv("CITRIC_DATA_DIR", t.TempDir())
	t.Setenv("CITRIC_PORT", "eighty")
	if _, err := Load(""); err == nil {
		t.Fatal("want an error for a non-numeric port")
	}
}

func TestLegacyKeycloakSectionStillWorks(t *testing.T) {
	p := writeConfig(t, "[server]\ndata_dir = \""+filepath.ToSlash(t.TempDir())+"\"\n"+
		"[keycloak]\nurl = \"https://auth.example.com/\"\nrealm = \"main\"\nclient_id = \"citric\"\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.Keycloak.Issuer, "https://auth.example.com/realms/main"; got != want {
		t.Errorf("issuer: got %q, want %q", got, want)
	}
	if !cfg.Keycloak.SSOEnabled() || !cfg.Keycloak.SyncEnabled() {
		t.Error("legacy keycloak config should enable SSO and sync")
	}
}

func TestOIDCSectionWithGenericIssuer(t *testing.T) {
	p := writeConfig(t, "[server]\ndata_dir = \""+filepath.ToSlash(t.TempDir())+"\"\n"+
		"[oidc]\nissuer = \"https://login.example.com/\"\nclient_id = \"citric\"\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keycloak.Issuer != "https://login.example.com" || !cfg.Keycloak.SSOEnabled() {
		t.Errorf("generic issuer not applied: %+v", cfg.Keycloak)
	}
	if cfg.Keycloak.SyncEnabled() {
		t.Error("sync must stay off without a Keycloak URL and realm")
	}
}
