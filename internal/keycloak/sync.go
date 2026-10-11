package keycloak

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/model"
	"github.com/blaxstar/citric/internal/store"
)

type kcUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Enabled  bool   `json:"enabled"`
}

// fetchUserRealmRoles returns the user's effective realm roles (incl. composite),
// used to cache role-based repo access. Best-effort: returns nil on any error.
func fetchUserRealmRoles(cfg config.Keycloak, token, userID string) []string {
	if userID == "" {
		return nil
	}
	u := fmt.Sprintf("%s/admin/realms/%s/users/%s/role-mappings/realm/composite", cfg.AdminURL(), cfg.Realm, userID)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var roles []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&roles); err != nil {
		return nil
	}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		if r.Name != "" {
			out = append(out, r.Name)
		}
	}
	return out
}

func fetchToken(cfg config.Keycloak) (string, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)

	tokenURL := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token", cfg.AdminURL(), cfg.Realm)
	resp, err := http.Post(tokenURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request %d: %s", resp.StatusCode, body)
	}
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	return result.AccessToken, nil
}

func fetchUsers(cfg config.Keycloak, token string) ([]kcUser, error) {
	usersURL := fmt.Sprintf("%s/admin/realms/%s/users?max=1000&enabled=true", cfg.AdminURL(), cfg.Realm)
	req, err := http.NewRequest("GET", usersURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("users request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("users request %d: %s", resp.StatusCode, body)
	}
	var users []kcUser
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	return users, nil
}

// fetchRoleMembers returns the set of usernames assigned the given realm role.
// A 404 (role does not exist) returns an empty set with a nil error so callers
// can distinguish "no members" from a transport failure.
func fetchRoleMembers(cfg config.Keycloak, token, role string) (map[string]bool, error) {
	roleURL := fmt.Sprintf("%s/admin/realms/%s/roles/%s/users?max=1000",
		cfg.AdminURL(), cfg.Realm, url.PathEscape(role))
	req, err := http.NewRequest("GET", roleURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("role members request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return map[string]bool{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("role members request %d: %s", resp.StatusCode, body)
	}
	var members []kcUser
	if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
		return nil, fmt.Errorf("decode role members: %w", err)
	}
	set := make(map[string]bool, len(members))
	for _, m := range members {
		if m.Username != "" {
			set[m.Username] = true
		}
	}
	return set, nil
}

// ListRealmRoles returns the realm's role names (for the repo-access UI). Filters
// out Keycloak's built-in default/uma/offline roles to keep the list useful.
func ListRealmRoles(cfg config.Keycloak) ([]string, error) {
	token, err := fetchToken(cfg)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/admin/realms/%s/roles?max=1000", cfg.AdminURL(), cfg.Realm)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("roles request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("roles request %d: %s", resp.StatusCode, body)
	}
	var roles []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&roles); err != nil {
		return nil, fmt.Errorf("decode roles: %w", err)
	}
	skip := map[string]bool{"offline_access": true, "uma_authorization": true}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		if r.Name == "" || skip[r.Name] || strings.HasPrefix(r.Name, "default-roles-") {
			continue
		}
		out = append(out, r.Name)
	}
	return out, nil
}

// Sync fetches all enabled users from Keycloak, upserts them into citric's users
// table, and reconciles roles: members of the configured admin realm role become
// admins, everyone else becomes a submitter. Role authority is centralized in
// Keycloak. If the admin role can't be fetched, roles are left untouched (so a
// transient Keycloak hiccup never mass-demotes admins).
func Sync(db *store.DB, cfg config.Keycloak) error {
	token, err := fetchToken(cfg)
	if err != nil {
		return err
	}
	users, err := fetchUsers(cfg, token)
	if err != nil {
		return err
	}

	admins, adminErr := fetchRoleMembers(cfg, token, cfg.AdminRole)
	if adminErr != nil {
		log.Printf("[keycloak] could not fetch %q members, leaving roles unchanged: %v", cfg.AdminRole, adminErr)
	}

	synced := 0
	for _, u := range users {
		if !u.Enabled || u.Username == "" {
			continue
		}
		// Skip Keycloak service accounts (clients' bot users) — not real people.
		if strings.HasPrefix(u.Username, "service-account-") {
			continue
		}
		if err := db.UpsertKeycloakUser(u.Username, u.Email); err != nil {
			log.Printf("[keycloak] upsert %s: %v", u.Username, err)
			continue
		}
		synced++
		if adminErr == nil {
			role := model.RoleSubmitter
			if admins[u.Username] {
				role = model.RoleAdmin
			}
			if err := db.SetUserRole(u.Username, role); err != nil {
				log.Printf("[keycloak] set role %s: %v", u.Username, err)
			}
		}
		// Cache effective realm roles for repo-access checks (best-effort).
		if roles := fetchUserRealmRoles(cfg, token, u.ID); roles != nil {
			if err := db.ReplaceUserRoles(u.Username, roles); err != nil {
				log.Printf("[keycloak] cache roles %s: %v", u.Username, err)
			}
		}
	}
	log.Printf("[keycloak] synced %d/%d users from realm %s (%d admins)", synced, len(users), cfg.Realm, len(admins))
	return nil
}

// RunSync performs an immediate sync on startup, then repeats every interval.
func RunSync(db *store.DB, cfg config.Keycloak, interval time.Duration) {
	run := func() {
		if err := Sync(db, cfg); err != nil {
			log.Printf("[keycloak] sync error: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		run()
	}
}
