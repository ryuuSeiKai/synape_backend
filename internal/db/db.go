// Package db provides PostgreSQL-backed storage for users, providers, and sync blobs.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Store wraps a PostgreSQL database connection.
type Store struct {
	DB *sql.DB
}

// New opens a PostgreSQL database and runs migrations.
func New(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) migrate() error {
	// Drop the legacy sync_blobs table if it exists but lacks user_id
	var hasUserID bool
	err := s.DB.QueryRow(`
		SELECT EXISTS (
			SELECT 1 
			FROM information_schema.columns 
			WHERE table_name='sync_blobs' AND column_name='user_id'
		)
	`).Scan(&hasUserID)
	if err == nil && !hasUserID {
		_, _ = s.DB.Exec(`DROP TABLE IF EXISTS sync_blobs CASCADE`)
	}

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			user_id BIGINT NOT NULL UNIQUE,
			email TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			first_name TEXT NOT NULL DEFAULT '',
			last_name TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			slug TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS providers (
			id SERIAL PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			provider_user_id TEXT NOT NULL,
			provider_login TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			linked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(user_id, provider)
		)`,
		`CREATE TABLE IF NOT EXISTS sync_blobs (
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (user_id, kind)
		)`,
	}

	for _, m := range migrations {
		if _, err := s.DB.Exec(m); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

// ─── User operations ──────────────────────────────────────────────────────

// GetUserByDocID returns a user by its document ID ("gh:12345" or "google:sub").
func (s *Store) GetUserByDocID(docID string) (*User, error) {
	u := &User{}
	err := s.DB.QueryRow(
		`SELECT id, user_id, email, display_name, first_name, last_name, avatar_url, slug, created_at, updated_at
		 FROM users WHERE id = $1`, docID,
	).Scan(&u.ID, &u.UserID, &u.Email, &u.DisplayName, &u.FirstName, &u.LastName, &u.AvatarURL, &u.Slug, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// GetUserByProviderUserID returns a user by provider + provider_user_id.
func (s *Store) GetUserByProviderUserID(provider, providerUserID string) (*User, error) {
	var docID string
	err := s.DB.QueryRow(
		`SELECT user_id FROM providers WHERE provider = $1 AND provider_user_id = $2`,
		provider, providerUserID,
	).Scan(&docID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GetUserByDocID(docID)
}

// UpsertUser inserts or updates a user, returning the user.
func (s *Store) UpsertUser(u *User) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if u.CreatedAt == "" {
		u.CreatedAt = now
	}
	u.UpdatedAt = now

	_, err := s.DB.Exec(
		`INSERT INTO users (id, user_id, email, display_name, first_name, last_name, avatar_url, slug, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT(id) DO UPDATE SET
			email=EXCLUDED.email,
			display_name=EXCLUDED.display_name,
			first_name=EXCLUDED.first_name,
			last_name=EXCLUDED.last_name,
			avatar_url=EXCLUDED.avatar_url,
			slug=EXCLUDED.slug,
			updated_at=EXCLUDED.updated_at`,
		u.ID, u.UserID, u.Email, u.DisplayName, u.FirstName, u.LastName, u.AvatarURL, u.Slug, u.CreatedAt, u.UpdatedAt,
	)
	return err
}

// UpdateUserProfile updates display_name, first_name, last_name for a user.
func (s *Store) UpdateUserProfile(docID string, displayName, firstName, lastName *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	query := "UPDATE users SET updated_at = $1"
	args := []interface{}{now}
	argSeq := 2

	if displayName != nil {
		query += fmt.Sprintf(", display_name = $%d", argSeq)
		args = append(args, *displayName)
		argSeq++
	}
	if firstName != nil {
		query += fmt.Sprintf(", first_name = $%d", argSeq)
		args = append(args, *firstName)
		argSeq++
	}
	if lastName != nil {
		query += fmt.Sprintf(", last_name = $%d", argSeq)
		args = append(args, *lastName)
		argSeq++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argSeq)
	args = append(args, docID)

	_, err := s.DB.Exec(query, args...)
	return err
}


// DeleteUser removes a user and their providers (providers cascade via FK).
func (s *Store) DeleteUser(docID string) error {
	_, err := s.DB.Exec(`DELETE FROM users WHERE id = $1`, docID)
	return err
}

// ─── Provider operations ──────────────────────────────────────────────────

// GetProviders returns all providers linked to a user.
func (s *Store) GetProviders(userDocID string) ([]Provider, error) {
	rows, err := s.DB.Query(
		`SELECT provider, provider_user_id, provider_login, email, linked_at, last_seen_at
		 FROM providers WHERE user_id = $1`, userDocID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []Provider
	for rows.Next() {
		var p Provider
		if err := rows.Scan(&p.Provider, &p.ProviderUserID, &p.ProviderLogin, &p.Email, &p.LinkedAt, &p.LastSeenAt); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, rows.Err()
}

// UpsertProvider inserts or updates a provider link.
func (s *Store) UpsertProvider(p *Provider) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if p.LinkedAt == "" {
		p.LinkedAt = now
	}
	p.LastSeenAt = now

	_, err := s.DB.Exec(
		`INSERT INTO providers (user_id, provider, provider_user_id, provider_login, email, linked_at, last_seen_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT(user_id, provider) DO UPDATE SET
			provider_user_id=EXCLUDED.provider_user_id,
			provider_login=EXCLUDED.provider_login,
			email=EXCLUDED.email,
			last_seen_at=EXCLUDED.last_seen_at`,
		p.UserID, p.Provider, p.ProviderUserID, p.ProviderLogin, p.Email, p.LinkedAt, p.LastSeenAt,
	)
	return err
}

// DeleteProvider removes a provider link.
func (s *Store) DeleteProvider(userDocID, provider string) error {
	_, err := s.DB.Exec(`DELETE FROM providers WHERE user_id = $1 AND provider = $2`, userDocID, provider)
	return err
}

// ─── Sync blob operations ─────────────────────────────────────────────────

// GetSyncBlobs returns all sync blobs for a user (state only).
func (s *Store) GetSyncBlobs(userDocID string) ([]SyncBlob, error) {
	rows, err := s.DB.Query(
		`SELECT kind, content_hash, updated_at FROM sync_blobs WHERE user_id = $1 ORDER BY kind`,
		userDocID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []SyncBlob
	for rows.Next() {
		var b SyncBlob
		if err := rows.Scan(&b.Kind, &b.ContentHash, &b.UpdatedAt); err != nil {
			return nil, err
		}
		blobs = append(blobs, b)
	}
	return blobs, rows.Err()
}

// GetSyncBlob returns a single sync blob by user and kind.
func (s *Store) GetSyncBlob(userDocID, kind string) (*SyncBlob, error) {
	b := &SyncBlob{}
	err := s.DB.QueryRow(
		`SELECT kind, content_hash, payload, created_at, updated_at FROM sync_blobs WHERE user_id = $1 AND kind = $2`,
		userDocID, kind,
	).Scan(&b.Kind, &b.ContentHash, &b.Payload, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

// UpsertSyncBlob inserts or updates a sync blob for a user.
func (s *Store) UpsertSyncBlob(userDocID, kind, contentHash, payload string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DB.Exec(
		`INSERT INTO sync_blobs (user_id, kind, content_hash, payload, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), $5)
		 ON CONFLICT(user_id, kind) DO UPDATE SET
			content_hash=EXCLUDED.content_hash,
			payload=EXCLUDED.payload,
			updated_at=$5`,
		userDocID, kind, contentHash, payload, now,
	)
	return err
}

// WipeSyncBlobs removes all sync blobs for a user.
func (s *Store) WipeSyncBlobs(userDocID string) error {
	_, err := s.DB.Exec(`DELETE FROM sync_blobs WHERE user_id = $1`, userDocID)
	return err
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func coalesce(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

// UserToCloudUser converts a db User to the API response shape.
func (u *User) UserToCloudUser() CloudUser {
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

// ProviderToCloudProvider converts a db Provider to the API response shape.
func (p *Provider) ProviderToCloudProvider() CloudProvider {
	return CloudProvider{
		Provider:       p.Provider,
		ProviderUserID: p.ProviderUserID,
		ProviderLogin:  strPtrOrNil(p.ProviderLogin),
		Email:          strPtrOrNil(p.Email),
		LinkedAt:       p.LinkedAt,
		LastSeenAt:     p.LastSeenAt,
	}
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// CloudUser is the API response shape for a user.
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

// CloudProvider is the API response shape for a provider.
type CloudProvider struct {
	Provider       string  `json:"provider"`
	ProviderUserID string  `json:"providerUserId"`
	ProviderLogin  *string `json:"providerLogin,omitempty"`
	Email          *string `json:"email,omitempty"`
	LinkedAt       string  `json:"linkedAt"`
	LastSeenAt     string  `json:"lastSeenAt"`
}
