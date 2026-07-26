package auth

// ─── Request types ─────────────────────────────────────────────────────────

type CodeExchangeRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirectUri,omitempty"`
}

type GoogleRefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type ProfileUpdateRequest struct {
	DisplayName *string `json:"displayName,omitempty"`
	FirstName   *string `json:"firstName,omitempty"`
	LastName    *string `json:"lastName,omitempty"`
}

type LinkRequest struct {
	Provider    string  `json:"provider"`
	Code        string  `json:"code"`
	RedirectURI *string `json:"redirectUri,omitempty"`
}

type UnlinkRequest struct {
	Provider string `json:"provider"`
}

// ─── Response types ────────────────────────────────────────────────────────

type CloudUser struct {
	UserID      int64   `json:"userId"`
	Email       *string `json:"email,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	FirstName   *string `json:"firstName,omitempty"`
	LastName    *string `json:"lastName,omitempty"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
	Slug        string  `json:"slug"`
	CreatedAt   *string `json:"createdAt,omitempty"`
}

type CloudProvider struct {
	Provider       string  `json:"provider"`
	ProviderUserID string  `json:"providerUserId"`
	ProviderLogin  *string `json:"providerLogin,omitempty"`
	Email          *string `json:"email,omitempty"`
	LinkedAt       string  `json:"linkedAt"`
	LastSeenAt     string  `json:"lastSeenAt"`
}

type CloudEntitlements struct {
	Plan         string           `json:"plan"`
	Credits      *CloudCredits    `json:"credits,omitempty"`
	Subscription *CloudSubscription `json:"subscription,omitempty"`
}

type CloudCredits struct {
	Remaining int64   `json:"remaining"`
	Allowance int64   `json:"allowance"`
	ResetsAt  *string `json:"resetsAt,omitempty"`
}

type CloudSubscription struct {
	Status            string `json:"status"`
	CancelAtPeriodEnd bool   `json:"cancelAtPeriodEnd"`
	IsLifetime        bool   `json:"isLifetime"`
	CurrentPeriodEnd  *string `json:"currentPeriodEnd,omitempty"`
	CurrentPeriodStart *string `json:"currentPeriodStart,omitempty"`
	Interval          *string `json:"interval,omitempty"`
	PriceUSD          *int64  `json:"priceUsd,omitempty"`
}

// AuthResponse is returned by /api/auth/{provider}/exchange.
type AuthResponse struct {
	Token       *string           `json:"token,omitempty"`
	Refresh     *string           `json:"refresh,omitempty"`
	IDToken     *string           `json:"idToken,omitempty"`
	ExpiresIn   *int64            `json:"expiresIn,omitempty"`
	User        CloudUser         `json:"user"`
	Providers   []CloudProvider   `json:"providers"`
	Plan        string            `json:"plan"`
	Entitlements CloudEntitlements `json:"entitlements"`
}

// MeResponse is returned by /api/auth/me (same shape minus tokens).
type MeResponse struct {
	User        CloudUser         `json:"user"`
	Providers   []CloudProvider   `json:"providers"`
	Plan        string            `json:"plan"`
	Entitlements CloudEntitlements `json:"entitlements"`
}

// GoogleRefreshResponse is returned by /api/auth/google/refresh.
type GoogleRefreshResponse struct {
	Token     string  `json:"token"`
	IDToken   *string `json:"idToken,omitempty"`
	ExpiresIn *int64  `json:"expiresIn,omitempty"`
}

// ─── GitHub access token response ─────────────────────────────────────────

type githubTokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	Scope           string `json:"scope"`
	Error           string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// ─── Google token response ─────────────────────────────────────────────────

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Error        string `json:"error,omitempty"`
}

// googleClaims represents the standard claims in a Google id_token JWT.
type googleClaims struct {
	Iss        string `json:"iss"`
	Aud        string `json:"aud"`
	Sub        string `json:"sub"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	Picture    string `json:"picture"`
	Exp        int64  `json:"exp"`
}
