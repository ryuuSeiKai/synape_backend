package db

// User represents a row in the users table.
type User struct {
	ID          string `json:"-" db:"id"`
	UserID      int64  `json:"userId" db:"user_id"`
	Email       string `json:"email,omitempty" db:"email"`
	DisplayName string `json:"displayName,omitempty" db:"display_name"`
	FirstName   string `json:"firstName,omitempty" db:"first_name"`
	LastName    string `json:"lastName,omitempty" db:"last_name"`
	AvatarURL   string `json:"avatarUrl,omitempty" db:"avatar_url"`
	Slug        string `json:"slug" db:"slug"`
	CreatedAt   string `json:"createdAt,omitempty" db:"created_at"`
	UpdatedAt   string `json:"-" db:"updated_at"`
}

// Provider represents a row in the providers table.
type Provider struct {
	ID             int64  `json:"-" db:"id"`
	UserID         string `json:"-" db:"user_id"`
	Provider       string `json:"provider" db:"provider"`
	ProviderUserID string `json:"providerUserId" db:"provider_user_id"`
	ProviderLogin  string `json:"providerLogin,omitempty" db:"provider_login"`
	Email          string `json:"email,omitempty" db:"email"`
	LinkedAt       string `json:"linkedAt" db:"linked_at"`
	LastSeenAt     string `json:"lastSeenAt" db:"last_seen_at"`
}

// SyncBlob represents a row in the sync_blobs table.
type SyncBlob struct {
	Kind        string `json:"kind" db:"kind"`
	ContentHash string `json:"contentHash" db:"content_hash"`
	Payload     string `json:"payload" db:"payload"`
	CreatedAt   string `json:"-" db:"created_at"`
	UpdatedAt   string `json:"updatedAt" db:"updated_at"`
}
