package api

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/blaxstar/citric/internal/auth"
	"github.com/blaxstar/citric/internal/config"
	"github.com/blaxstar/citric/internal/model"
	"github.com/golang-jwt/jwt/v5"
)

// sessionCookie is the httpOnly cookie holding citric's own session JWT after SSO.
const sessionCookie = "crit_session"
const oidcStateCookie = "crit_oidc_state"

// oidcProvider talks to the OIDC provider for the authorization-code + PKCE
// login flow.
// It caches OIDC discovery and JWKS, and tracks in-flight login states.
type oidcProvider struct {
	cfg    config.Keycloak
	secret string // citric JWT signing secret (for the session token)
	ttl    string // session token TTL

	mu     sync.Mutex
	disco  *discoveryDoc
	jwks   map[string]*rsa.PublicKey
	states map[string]*loginState
}

type discoveryDoc struct {
	Issuer        string `json:"issuer"`
	AuthEndpoint  string `json:"authorization_endpoint"`
	TokenEndpoint string `json:"token_endpoint"`
	JWKSURI       string `json:"jwks_uri"`
	EndSession    string `json:"end_session_endpoint"`
}

type loginState struct {
	verifier  string
	nonce     string
	returnTo  string
	expiresAt time.Time
}

func newOIDCProvider(kc config.Keycloak, secret, ttl string) *oidcProvider {
	p := &oidcProvider{
		cfg:    kc,
		secret: secret,
		ttl:    ttl,
		jwks:   map[string]*rsa.PublicKey{},
		states: map[string]*loginState{},
	}
	go p.sweep()
	return p
}

func (p *oidcProvider) sweep() {
	for range time.Tick(2 * time.Minute) {
		p.mu.Lock()
		for k, s := range p.states {
			if time.Now().After(s.expiresAt) {
				delete(p.states, k)
			}
		}
		p.mu.Unlock()
	}
}

// discovery fetches and caches the realm's OIDC metadata.
func (p *oidcProvider) discovery() (*discoveryDoc, error) {
	p.mu.Lock()
	if p.disco != nil {
		d := p.disco
		p.mu.Unlock()
		return d, nil
	}
	p.mu.Unlock()

	u := p.cfg.Issuer + "/.well-known/openid-configuration"
	resp, err := http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oidc discovery: status %d", resp.StatusCode)
	}
	var d discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("oidc discovery decode: %w", err)
	}
	p.mu.Lock()
	p.disco = &d
	p.mu.Unlock()
	return &d, nil
}

// publicKey returns the RSA key for the given kid, fetching JWKS if needed.
func (p *oidcProvider) publicKey(kid string) (*rsa.PublicKey, error) {
	p.mu.Lock()
	if k, ok := p.jwks[kid]; ok {
		p.mu.Unlock()
		return k, nil
	}
	p.mu.Unlock()

	d, err := p.discovery()
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(d.JWKSURI)
	if err != nil {
		return nil, fmt.Errorf("jwks fetch: %w", err)
	}
	defer resp.Body.Close()

	var doc struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("jwks decode: %w", err)
	}

	p.mu.Lock()
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		p.jwks[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nb),
			E: int(new(big.Int).SetBytes(eb).Int64()),
		}
	}
	key, ok := p.jwks[kid]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("jwks: no key for kid %q", kid)
	}
	return key, nil
}

func (p *oidcProvider) newState(returnTo string) (state, challenge, nonce string) {
	verifier := randURL(48)
	nonce = randURL(24)
	state = randURL(24)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])

	p.mu.Lock()
	p.states[state] = &loginState{
		verifier:  verifier,
		nonce:     nonce,
		returnTo:  returnTo,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
	p.mu.Unlock()
	return state, challenge, nonce
}

func (p *oidcProvider) consumeState(state string) (*loginState, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.states[state]
	if !ok || time.Now().After(s.expiresAt) {
		delete(p.states, state)
		return nil, false
	}
	delete(p.states, state)
	return s, true
}

func randURL(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// reqBaseURL reconstructs the externally-visible base URL (scheme://host) so the
// redirect_uri matches what the browser used, on both corp and public domains.
func reqBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// ── handlers ──────────────────────────────────────────────────────────────────

// handleSSOLogin starts the OIDC auth-code + PKCE flow by redirecting to the
// identity provider.
// GET /auth/sso/login?return_to=/some/path
func (s *Server) handleSSOLogin(w http.ResponseWriter, r *http.Request) {
	d, err := s.oidc.discovery()
	if err != nil {
		http.Error(w, "sso unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}

	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/") {
		returnTo = "/"
	}
	state, challenge, nonce := s.oidc.newState(returnTo)

	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	redirectURI := reqBaseURL(r) + "/auth/callback"
	q := url.Values{
		"client_id":             {s.oidc.cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	// Allow callers to pass prompt=login or prompt=select_account to force
	// Keycloak to show the login screen even with an active session.
	if p := r.URL.Query().Get("prompt"); p == "login" || p == "select_account" {
		q.Set("prompt", p)
	}
	http.Redirect(w, r, d.AuthEndpoint+"?"+q.Encode(), http.StatusFound)
}

// handleSSOCallback completes the flow: validates state, exchanges the code,
// verifies the ID token, upserts the user, and sets the session cookie.
// GET /auth/callback?code=...&state=...
func (s *Server) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		http.Error(w, "sso error: "+e+" "+q.Get("error_description"), http.StatusUnauthorized)
		return
	}
	state := q.Get("state")
	code := q.Get("code")
	if state == "" || code == "" {
		http.Error(w, "missing state or code", http.StatusBadRequest)
		return
	}

	// CSRF: the state in the URL must match the one we set in the cookie.
	c, err := r.Cookie(oidcStateCookie)
	if err != nil || c.Value != state {
		http.Error(w, "state mismatch", http.StatusUnauthorized)
		return
	}
	ls, ok := s.oidc.consumeState(state)
	if !ok {
		http.Error(w, "expired or unknown login state", http.StatusUnauthorized)
		return
	}

	d, err := s.oidc.discovery()
	if err != nil {
		http.Error(w, "sso unavailable", http.StatusBadGateway)
		return
	}

	// Exchange the authorization code for tokens (confidential client).
	redirectURI := reqBaseURL(r) + "/auth/callback"
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {s.oidc.cfg.ClientID},
		"client_secret": {s.oidc.cfg.ClientSecret},
		"code_verifier": {ls.verifier},
	}
	resp, err := http.PostForm(d.TokenEndpoint, form)
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		http.Error(w, "token exchange rejected", http.StatusUnauthorized)
		return
	}
	var tok struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.IDToken == "" {
		http.Error(w, "bad token response", http.StatusBadGateway)
		return
	}

	username, email, idClaims, err := s.oidc.verifyIDToken(tok.IDToken, ls.nonce, d.Issuer)
	if err != nil {
		http.Error(w, "id token rejected: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Identity comes from the identity provider, and so does authorization: the
	// configured admin role grants admin, everyone else is a submitter.
	if err := s.db.UpsertKeycloakUser(username, email); err != nil {
		http.Error(w, "could not provision user", http.StatusInternalServerError)
		return
	}
	// Decide the role from the SSO token. If we can't read roles (parse/verify
	// error), leave the existing DB role untouched rather than demoting.
	if roles, rerr := s.oidc.effectiveRoles(tok.AccessToken, idClaims, d.Issuer, s.oidc.cfg.ClientID); rerr != nil {
		log.Printf("[oidc] %s: could not read roles, keeping existing role: %v", username, rerr)
	} else {
		role := model.RoleSubmitter
		for _, rl := range roles {
			if rl == s.oidc.cfg.AdminRole {
				role = model.RoleAdmin
				break
			}
		}
		log.Printf("[oidc] %s roles=%v adminRole=%q -> %s", username, roles, s.oidc.cfg.AdminRole, role)
		if err := s.db.SetUserRole(username, role); err != nil {
			http.Error(w, "could not set role", http.StatusInternalServerError)
			return
		}
		// Cache effective roles so the git path (username-only token) can resolve
		// role-based repo access.
		if err := s.db.ReplaceUserRoles(username, roles); err != nil {
			log.Printf("[oidc] %s: cache roles failed: %v", username, err)
		}
	}

	token, err := auth.IssueJWT(username, s.oidc.secret, s.oidc.ttl)
	if err != nil {
		http.Error(w, "session issue failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge(s.oidc.ttl).Seconds()),
	})
	// clear the one-shot state cookie
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Path: "/", MaxAge: -1})

	// SPA uses hash routing; land them where they started.
	dest := "/"
	if ls.returnTo != "" && ls.returnTo != "/" {
		dest = "/#" + ls.returnTo
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// handleAuthConfig tells the SPA how to log in. Public (no auth).
// GET /auth/config
func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	title, _ := s.db.GetSetting("site_title")
	if title == "" {
		title = "CitriC"
	}
	palette, _ := s.db.GetSetting("default_palette")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"sso":             s.oidc != nil,
		"user_sync":       s.cfg.Keycloak.SyncEnabled(),
		"default_palette": palette,
		"login_url":       "/auth/sso/login",
		"site_title":      title,
	})
}

// handleLogout clears the citric session. Keycloak's own session is left intact
// (re-login is silent); the CLI remains usable as a break-glass path.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: true,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// verifyIDToken validates the ID token signature (via JWKS) and core claims,
// returning the username, email and the full claim set. The username is
// preferred_username when the provider sends one (Keycloak, Authentik, …),
// else the local part of the email, else the subject.
func (p *oidcProvider) verifyIDToken(raw, nonce, issuer string) (username, email string, claims jwt.MapClaims, err error) {
	claims = jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(raw, claims, p.keyFunc, jwt.WithIssuer(issuer), jwt.WithAudience(p.cfg.ClientID))
	if err != nil {
		return "", "", nil, err
	}
	if n, _ := claims["nonce"].(string); n != nonce {
		return "", "", nil, fmt.Errorf("nonce mismatch")
	}
	email, _ = claims["email"].(string)
	username, _ = claims["preferred_username"].(string)
	if username == "" && email != "" {
		username, _, _ = strings.Cut(email, "@")
	}
	if username == "" {
		username, _ = claims["sub"].(string)
	}
	if username == "" {
		return "", "", nil, fmt.Errorf("token has no preferred_username, email or sub")
	}
	return username, email, claims, nil
}

func (p *oidcProvider) keyFunc(t *jwt.Token) (any, error) {
	if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
		return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
	}
	kid, _ := t.Header["kid"].(string)
	return p.publicKey(kid)
}

// effectiveRoles returns the user's role names, read from the access token
// when it is a JWT the provider signed, and from the (already verified) ID
// token's claims. Keycloak puts roles in realm_access.roles and
// resource_access[clientID].roles, so the admin role works whether it's a
// realm role or a client role on the citric client; other providers commonly
// use "roles" or "groups", and roles_claim adds any other claim. Audience is
// not checked on the access token — Keycloak audiences it to the resource
// server ("account"), not this client.
func (p *oidcProvider) effectiveRoles(rawAccess string, idClaims jwt.MapClaims, issuer, clientID string) ([]string, error) {
	var sets []jwt.MapClaims
	if rawAccess != "" {
		access := jwt.MapClaims{}
		if _, err := jwt.ParseWithClaims(rawAccess, access, p.keyFunc, jwt.WithIssuer(issuer)); err == nil {
			sets = append(sets, access)
		}
		// An opaque (non-JWT) access token is normal for many providers; the
		// ID token's claims below still apply.
	}
	if idClaims != nil {
		sets = append(sets, idClaims)
	}
	if len(sets) == 0 {
		return nil, fmt.Errorf("no verifiable token to read roles from")
	}
	seen := map[string]bool{}
	var roles []string
	for _, c := range sets {
		for _, r := range rolesFromClaims(c, clientID, p.cfg.RolesClaim) {
			if !seen[r] {
				seen[r] = true
				roles = append(roles, r)
			}
		}
	}
	return roles, nil
}

// rolesFromClaims collects role names from the claim locations providers use.
func rolesFromClaims(c jwt.MapClaims, clientID, extraClaim string) []string {
	var out []string
	add := func(v any) {
		switch list := v.(type) {
		case []any:
			for _, r := range list {
				if s, ok := r.(string); ok && s != "" {
					out = append(out, s)
				}
			}
		case []string:
			out = append(out, list...)
		case string:
			if list != "" {
				out = append(out, list)
			}
		}
	}
	add(claimPath(c, "realm_access.roles"))
	if ra, ok := c["resource_access"].(map[string]any); ok {
		if client, ok := ra[clientID].(map[string]any); ok {
			add(client["roles"])
		}
	}
	add(c["roles"])
	add(c["groups"])
	if extraClaim != "" {
		add(claimPath(c, extraClaim))
	}
	return out
}

// claimPath resolves a dot-separated path ("realm_access.roles") in claims.
func claimPath(c jwt.MapClaims, path string) any {
	var cur any = map[string]any(c)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func sessionMaxAge(ttl string) time.Duration {
	if d, err := time.ParseDuration(ttl); err == nil && d > 0 {
		return d
	}
	return 720 * time.Hour
}
