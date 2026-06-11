// Package auth implements OAuth authentication handlers for GitHub and Google.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ryuuvu/synape-server/internal/config"
	"github.com/ryuuvu/synape-server/internal/db"
)

// Handler holds dependencies for auth HTTP handlers.
type Handler struct {
	Store  *db.Store
	Config config.Config
}

// NewHandler creates a new auth Handler.
func NewHandler(store *db.Store, cfg config.Config) *Handler {
	return &Handler{Store: store, Config: cfg}
}

// ─── OAuth Callback Redirects ──────────────────────────────────────────────

// CallbackRedirect handles GET /auth/callback (GitHub OAuth callback).
// Redirects to deep-link: Synape://oauth-callback?provider=github&code=...
func (h *Handler) CallbackRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}
	redirect := fmt.Sprintf("%s://oauth-callback?provider=github&code=%s",
		h.Config.DeepLinkScheme, urlencode(code))
	log.Printf("[oauth] GitHub callback → redirecting to deep-link")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><script>window.location.href=%s</script></head><body></body></html>`,
		jsonEncode(redirect))
}

// GoogleCallbackRedirect handles GET /auth/google-callback.html.
func (h *Handler) GoogleCallbackRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}
	redirect := fmt.Sprintf("%s://oauth-callback?provider=google&code=%s",
		h.Config.DeepLinkScheme, urlencode(code))
	log.Printf("[oauth] Google callback → redirecting to deep-link")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><script>window.location.href=%s</script></head><body></body></html>`,
		jsonEncode(redirect))
}

// ─── Token Exchange ────────────────────────────────────────────────────────

// GitHubExchange handles POST /api/auth/github/exchange.
func (h *Handler) GitHubExchange(w http.ResponseWriter, r *http.Request) {
	var req CodeExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing code"})
		return
	}

	// Exchange code for access token
	tok, err := exchangeGitHubCode(req.Code, h.Config)
	if err != nil {
		log.Printf("[auth] GitHub exchange failed: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Code exchange failed", "detail": err.Error()})
		return
	}

	// Fetch GitHub user
	ghUser, err := fetchGitHubUser(tok)
	if err != nil {
		log.Printf("[auth] GitHub user fetch failed: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub user fetch failed"})
		return
	}

	// Fetch primary email
	email := ghUser.Email
	if email == "" {
		if e, err := fetchGitHubPrimaryEmail(tok); err == nil {
			email = e
		}
	}

	docID := fmt.Sprintf("gh:%d", ghUser.ID)
	now := time.Now().UTC().Format(time.RFC3339)

	existing, _ := h.Store.GetUserByDocID(docID)
	createdAt := now
	if existing != nil {
		createdAt = existing.CreatedAt
	}

	user := &db.User{
		ID:          docID,
		UserID:      ghUser.ID,
		Email:       email,
		DisplayName: ghUser.Login,
		FirstName:   ghUser.Name,
		LastName:    "",
		AvatarURL:   ghUser.AvatarURL,
		Slug:        ghUser.Login,
		CreatedAt:   createdAt,
	}
	if user.FirstName == "" {
		user.FirstName = ghUser.Login
	}

	if err := h.Store.UpsertUser(user); err != nil {
		log.Printf("[auth] upsert user failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save user"})
		return
	}

	// Upsert provider
	prov := &db.Provider{
		UserID:         docID,
		Provider:       "github",
		ProviderUserID: fmt.Sprintf("%d", ghUser.ID),
		ProviderLogin:  ghUser.Login,
		Email:          email,
		LinkedAt:       createdAt,
	}
	if err := h.Store.UpsertProvider(prov); err != nil {
		log.Printf("[auth] upsert provider failed: %v", err)
	}

	providers, _ := h.Store.GetProviders(docID)

	resp := AuthResponse{
		Token:     &tok,
		Providers: toCloudProviders(providers),
		Plan:      "free",
		Entitlements: CloudEntitlements{
			Plan:    "free",
			Credits: nil,
		},
	}
	resp.User = toCloudUser(user)
	if resp.User.CreatedAt == nil || *resp.User.CreatedAt == "" {
		resp.User.CreatedAt = &createdAt
	}

	writeJSON(w, http.StatusOK, resp)
}

// GoogleExchange handles POST /api/auth/google/exchange.
func (h *Handler) GoogleExchange(w http.ResponseWriter, r *http.Request) {
	var req CodeExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing code"})
		return
	}

	redirectURI := req.RedirectURI
	if redirectURI == "" {
		scheme := "https"
		if r.TLS != nil {
			scheme = "https"
		} else if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		}
		host := r.Host
		redirectURI = fmt.Sprintf("%s://%s/auth/google-callback.html", scheme, host)
	}

	tok, err := exchangeGoogleCode(req.Code, redirectURI, h.Config)
	if err != nil {
		log.Printf("[auth] Google exchange failed: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google exchange failed", "detail": err.Error()})
		return
	}

	// Decode id_token JWT (no signature verification — just extract claims)
	claims, err := decodeGoogleIDToken(tok.IDToken)
	if err != nil {
		log.Printf("[auth] Google id_token decode failed: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google id_token decode failed"})
		return
	}

	userID := googleSubToUserID(claims.Sub)
	docID := fmt.Sprintf("google:%s", claims.Sub)
	now := time.Now().UTC().Format(time.RFC3339)

	existing, _ := h.Store.GetUserByDocID(docID)
	createdAt := now
	if existing != nil {
		createdAt = existing.CreatedAt
	}

	user := &db.User{
		ID:          docID,
		UserID:      userID,
		Email:       claims.Email,
		DisplayName: claims.Name,
		FirstName:   claims.GivenName,
		LastName:    claims.FamilyName,
		AvatarURL:   claims.Picture,
		Slug:        emailToSlug(claims.Email),
		CreatedAt:   createdAt,
	}

	if err := h.Store.UpsertUser(user); err != nil {
		log.Printf("[auth] upsert user failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save user"})
		return
	}

	prov := &db.Provider{
		UserID:         docID,
		Provider:       "google",
		ProviderUserID: claims.Sub,
		ProviderLogin:  claims.Name,
		Email:          claims.Email,
		LinkedAt:       createdAt,
	}
	if err := h.Store.UpsertProvider(prov); err != nil {
		log.Printf("[auth] upsert provider failed: %v", err)
	}

	providers, _ := h.Store.GetProviders(docID)

	resp := AuthResponse{
		Token:     &tok.AccessToken,
		IDToken:   &tok.IDToken,
		Refresh:   strPtr(tok.RefreshToken),
		ExpiresIn: &tok.ExpiresIn,
		Providers: toCloudProviders(providers),
		Plan:      "free",
		Entitlements: CloudEntitlements{
			Plan:    "free",
			Credits: nil,
		},
	}
	resp.User = toCloudUser(user)
	if resp.User.CreatedAt == nil || *resp.User.CreatedAt == "" {
		resp.User.CreatedAt = &createdAt
	}

	writeJSON(w, http.StatusOK, resp)
}

// GoogleRefresh handles POST /api/auth/google/refresh.
func (h *Handler) GoogleRefresh(w http.ResponseWriter, r *http.Request) {
	var req GoogleRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Missing refreshToken"})
		return
	}

	data, err := refreshGoogleToken(req.RefreshToken, h.Config)
	if err != nil {
		log.Printf("[auth] Google refresh failed: %v", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Refresh failed"})
		return
	}

	resp := GoogleRefreshResponse{
		Token:     data.AccessToken,
		ExpiresIn: &data.ExpiresIn,
	}
	if data.IDToken != "" {
		resp.IDToken = &data.IDToken
	}

	writeJSON(w, http.StatusOK, resp)
}

// ─── Auth Me ───────────────────────────────────────────────────────────────

// Me handles GET /api/auth/me — returns user info + entitlements.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	token, provider, err := extractAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	userID, err := validateToken(token, provider)
	if err != nil {
		log.Printf("[auth] token validation failed: %v", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Token expired"})
		return
	}

	docID := fmt.Sprintf("%s:%s", provider, userID)
	user, err := h.Store.GetUserByDocID(docID)
	if err != nil {
		log.Printf("[auth] user lookup failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "User not found"})
		return
	}

	providers, _ := h.Store.GetProviders(docID)

	resp := MeResponse{
		Providers: toCloudProviders(providers),
		Plan:      "free",
		Entitlements: CloudEntitlements{
			Plan:    "free",
			Credits: nil,
		},
	}
	resp.User = toCloudUser(user)

	writeJSON(w, http.StatusOK, resp)
}

// UpdateProfile handles PATCH /api/auth/me.
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	token, provider, err := extractAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	userID, err := validateToken(token, provider)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Token expired"})
		return
	}

	docID := fmt.Sprintf("%s:%s", provider, userID)

	var req ProfileUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	if err := h.Store.UpdateUserProfile(docID, req.DisplayName, req.FirstName, req.LastName); err != nil {
		log.Printf("[auth] update profile failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Fetch updated user
	user, _ := h.Store.GetUserByDocID(docID)
	providers, _ := h.Store.GetProviders(docID)

	resp := MeResponse{
		Providers: toCloudProviders(providers),
		Plan:      "free",
		Entitlements: CloudEntitlements{
			Plan:    "free",
			Credits: nil,
		},
	}
	if user != nil {
		resp.User = toCloudUser(user)
	}

	writeJSON(w, http.StatusOK, resp)
}

// DeleteAccount handles DELETE /api/auth/me.
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	token, provider, err := extractAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	userID, err := validateToken(token, provider)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Token expired"})
		return
	}

	confirmSlug := r.Header.Get("X-Confirm")
	docID := fmt.Sprintf("%s:%s", provider, userID)

	user, err := h.Store.GetUserByDocID(docID)
	if err != nil || user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "User not found"})
		return
	}

	if confirmSlug != "" && confirmSlug != user.Slug {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Slug does not match"})
		return
	}

	if err := h.Store.DeleteUser(docID); err != nil {
		log.Printf("[auth] delete account failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── Link / Unlink ─────────────────────────────────────────────────────────

// Link handles POST /api/auth/link — links another provider to existing account.
func (h *Handler) Link(w http.ResponseWriter, r *http.Request) {
	token, activeProvider, err := extractAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	userID, err := validateToken(token, activeProvider)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Token expired"})
		return
	}

	docID := fmt.Sprintf("%s:%s", activeProvider, userID)

	var req LinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	// Exchange code for the new provider
	var newProviderUserID, newProviderLogin, newEmail string
	now := time.Now().UTC().Format(time.RFC3339)

	switch req.Provider {
	case "github":
		tok, err := exchangeGitHubCode(req.Code, h.Config)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub exchange failed"})
			return
		}
		gh, err := fetchGitHubUser(tok)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub user fetch failed"})
			return
		}
		newProviderUserID = fmt.Sprintf("%d", gh.ID)
		newProviderLogin = gh.Login
		prov := &db.Provider{
			UserID:         docID,
			Provider:       "github",
			ProviderUserID: newProviderUserID,
			ProviderLogin:  newProviderLogin,
			Email:          newEmail,
			LinkedAt:       now,
		}
		if err := h.Store.UpsertProvider(prov); err != nil {
			log.Printf("[auth] link github provider failed: %v", err)
		}

	case "google":
		redirectURI := ""
		if req.RedirectURI != nil {
			redirectURI = *req.RedirectURI
		}
		if redirectURI == "" {
			redirectURI = fmt.Sprintf("%s/auth/google-callback.html", r.Header.Get("Origin"))
		}

		tok, err := exchangeGoogleCode(req.Code, redirectURI, h.Config)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google exchange failed"})
			return
		}
		claims, err := decodeGoogleIDToken(tok.IDToken)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Google id_token decode failed"})
			return
		}
		newProviderUserID = claims.Sub
		newProviderLogin = claims.Name
		newEmail = claims.Email
		prov := &db.Provider{
			UserID:         docID,
			Provider:       "google",
			ProviderUserID: newProviderUserID,
			ProviderLogin:  newProviderLogin,
			Email:          newEmail,
			LinkedAt:       now,
		}
		if err := h.Store.UpsertProvider(prov); err != nil {
			log.Printf("[auth] link google provider failed: %v", err)
		}

	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
		return
	}

	providers, _ := h.Store.GetProviders(docID)
	user, _ := h.Store.GetUserByDocID(docID)

	resp := MeResponse{
		Providers: toCloudProviders(providers),
		Plan:      "free",
		Entitlements: CloudEntitlements{
			Plan:    "free",
			Credits: nil,
		},
	}
	if user != nil {
		resp.User = toCloudUser(user)
	}

	writeJSON(w, http.StatusOK, resp)
}

// Unlink handles POST /api/auth/unlink.
func (h *Handler) Unlink(w http.ResponseWriter, r *http.Request) {
	token, provider, err := extractAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	userID, err := validateToken(token, provider)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Token expired"})
		return
	}

	docID := fmt.Sprintf("%s:%s", provider, userID)

	var req UnlinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	if err := h.Store.DeleteProvider(docID, req.Provider); err != nil {
		log.Printf("[auth] unlink provider failed: %v", err)
	}

	providers, _ := h.Store.GetProviders(docID)
	user, _ := h.Store.GetUserByDocID(docID)

	resp := MeResponse{
		Providers: toCloudProviders(providers),
		Plan:      "free",
		Entitlements: CloudEntitlements{
			Plan:    "free",
			Credits: nil,
		},
	}
	if user != nil {
		resp.User = toCloudUser(user)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ─── OAuth HTTP helpers ────────────────────────────────────────────────────

func exchangeGitHubCode(code string, cfg config.Config) (string, error) {
	body := fmt.Sprintf(
		`{"client_id":"%s","client_secret":"%s","code":"%s"}`,
		cfg.GitHubClientID, cfg.GitHubSecret, code,
	)
	resp, err := http.Post(
		"https://github.com/login/oauth/access_token",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	var tok githubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if tok.Error != "" {
		return "", fmt.Errorf("%s: %s", tok.Error, tok.ErrorDescription)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("empty access_token")
	}
	return tok.AccessToken, nil
}

func fetchGitHubUser(token string) (*githubUser, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	var u githubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if u.ID == 0 {
		return nil, fmt.Errorf("invalid user response")
	}
	return &u, nil
}

func fetchGitHubPrimaryEmail(token string) (string, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var emails []githubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	return "", fmt.Errorf("no primary verified email")
}

func exchangeGoogleCode(code, redirectURI string, cfg config.Config) (*googleTokenResponse, error) {
	body := fmt.Sprintf(
		"code=%s&client_id=%s&client_secret=%s&redirect_uri=%s&grant_type=authorization_code",
		urlencode(code), cfg.GoogleClientID, cfg.GoogleSecret, urlencode(redirectURI),
	)
	resp, err := http.Post(
		"https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	var tok googleTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if tok.Error != "" {
		return nil, fmt.Errorf("oauth error: %s", tok.Error)
	}
	if tok.IDToken == "" {
		return nil, fmt.Errorf("empty id_token")
	}
	return &tok, nil
}

func refreshGoogleToken(refreshToken string, cfg config.Config) (*googleTokenResponse, error) {
	body := fmt.Sprintf(
		"client_id=%s&client_secret=%s&refresh_token=%s&grant_type=refresh_token",
		cfg.GoogleClientID, cfg.GoogleSecret, refreshToken,
	)
	resp, err := http.Post(
		"https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	var tok googleTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if tok.Error != "" {
		return nil, fmt.Errorf("oauth error: %s", tok.Error)
	}
	return &tok, nil
}

func decodeGoogleIDToken(idToken string) (*googleClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT: expected 3 parts, got %d", len(parts))
	}

	// Decode payload (part 2)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Try with padding
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("decode payload: %w", err)
		}
	}

	var claims googleClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	// Verify not expired
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}

	return &claims, nil
}

// ─── Token validation ──────────────────────────────────────────────────────

// validateToken returns the provider's user ID string (e.g. GitHub user ID as string, Google sub).
func validateToken(token string, provider string) (string, error) {
	switch provider {
	case "github":
		return validateGitHubToken(token)
	case "google":
		return validateGoogleToken(token)
	default:
		return "", fmt.Errorf("unknown provider: %s", provider)
	}
}

func validateGitHubToken(token string) (string, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return "", fmt.Errorf("token expired")
	}

	var u githubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	if u.ID == 0 {
		return "", fmt.Errorf("invalid user")
	}
	return fmt.Sprintf("%d", u.ID), nil
}

func validateGoogleToken(token string) (string, error) {
	claims, err := decodeGoogleIDToken(token)
	if err != nil {
		return "", err
	}
	return claims.Sub, nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func extractAuth(r *http.Request) (token, provider string, err error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", "", fmt.Errorf("missing or invalid Authorization header")
	}
	token = strings.TrimPrefix(auth, "Bearer ")
	provider = r.Header.Get("X-Provider")
	if provider == "" {
		provider = "github"
	}
	return token, provider, nil
}

func toCloudUser(u *db.User) CloudUser {
	return CloudUser{
		UserID:      u.UserID,
		Email:       strPtrOrNil(u.Email),
		DisplayName: strPtrOrNil(u.DisplayName),
		FirstName:   strPtrOrNil(u.FirstName),
		LastName:    strPtrOrNil(u.LastName),
		AvatarURL:   strPtrOrNil(u.AvatarURL),
		Slug:        u.Slug,
		CreatedAt:   strPtrOrNil(u.CreatedAt),
	}
}

func toCloudProviders(providers []db.Provider) []CloudProvider {
	result := make([]CloudProvider, len(providers))
	for i, p := range providers {
		result[i] = CloudProvider{
			Provider:       p.Provider,
			ProviderUserID: p.ProviderUserID,
			ProviderLogin:  strPtrOrNil(p.ProviderLogin),
			Email:          strPtrOrNil(p.Email),
			LinkedAt:       p.LinkedAt,
			LastSeenAt:     p.LastSeenAt,
		}
	}
	return result
}

func googleSubToUserID(sub string) int64 {
	h := hmac.New(sha256.New, []byte("synape-user-id"))
	h.Write([]byte(sub))
	hash := h.Sum(nil)
	var id int64
	for i := 0; i < 8 && i < len(hash); i++ {
		id = (id << 8) | int64(hash[i])
	}
	if id < 0 {
		id = -id
	}
	return id
}

func emailToSlug(email string) string {
	if idx := strings.Index(email, "@"); idx > 0 {
		return email[:idx]
	}
	return "user"
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strPtr(s string) *string {
	return &s
}

func urlencode(s string) string {
	// Minimal URL encoding for OAuth params
	var out strings.Builder
	for _, b := range []byte(s) {
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '_' || b == '.' || b == '~' {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

func jsonEncode(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
