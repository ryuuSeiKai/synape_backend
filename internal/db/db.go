// Package db provides SQLite-backed storage for users, providers, and sync blobs.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database connection.
type Store struct {
	DB *sql.DB
}

// New opens (or creates) the SQLite database at path and runs migrations.
func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes

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
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL UNIQUE,
			email TEXT NOT NULL DEFAULT '',
			display_name TEXT NOT NULL DEFAULT '',
			first_name TEXT NOT NULL DEFAULT '',
			last_name TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			slug TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL REFERENCES users(id),
			provider TEXT NOT NULL,
			provider_user_id TEXT NOT NULL,
			provider_login TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			linked_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			UNIQUE(user_id, provider)
		)`,
		`CREATE TABLE IF NOT EXISTS sync_blobs (
			kind TEXT PRIMARY KEY,
			content_hash TEXT NOT NULL,
			payload TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
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
		 FROM users WHERE id = ?`, docID,
	).Scan(&u.ID, &u.UserID, &u.Email, &u.DisplayName, &u.FirstName, &u.LastName, &u.AvatarURL, &u.Slug, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// GetUserByProviderUserID returns a user by provider + provider_user_id.
// Used when re-authenticating with a known provider identity.
func (s *Store) GetUserByProviderUserID(provider, providerUserID string) (*User, error) {
	var docID string
	err := s.DB.QueryRow(
		`SELECT user_id FROM providers WHERE provider = ? AND provider_user_id = ?`,
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			email=excluded.email,
			display_name=excluded.display_name,
			first_name=excluded.first_name,
			last_name=excluded.last_name,
			avatar_url=excluded.avatar_url,
			slug=excluded.slug,
			updated_at=excluded.updated_at`,
		u.ID, u.UserID, u.Email, u.DisplayName, u.FirstName, u.LastName, u.AvatarURL, u.Slug, u.CreatedAt, u.UpdatedAt,
	)
	return err
}

// UpdateUserProfile updates display_name, first_name, last_name for a user.
func (s *Store) UpdateUserProfile(docID string, displayName, firstName, lastName *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DB.Exec(
		`UPDATE users SET display_name=?, first_name=?, last_name=?, updated_at=? WHERE id=?`,
		coalesce(displayName, ""), coalesce(firstName, ""), coalesce(lastName, ""), now, docID,
	)
	return err
}

// DeleteUser removes a user and their providers.
func (s *Store) DeleteUser(docID string) error {
	if _, err := s.DB.Exec(`DELETE FROM providers WHERE user_id = ?`, docID); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM users WHERE id = ?`, docID)
	return err
}

// ─── Provider operations ──────────────────────────────────────────────────

// GetProviders returns all providers linked to a user.
func (s *Store) GetProviders(userDocID string) ([]Provider, error) {
	rows, err := s.DB.Query(
		`SELECT provider, provider_user_id, provider_login, email, linked_at, last_seen_at
		 FROM providers WHERE user_id = ?`, userDocID,
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
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, provider) DO UPDATE SET
			provider_user_id=excluded.provider_user_id,
			provider_login=excluded.provider_login,
			email=excluded.email,
			last_seen_at=excluded.last_seen_at`,
		p.UserID, p.Provider, p.ProviderUserID, p.ProviderLogin, p.Email, p.LinkedAt, p.LastSeenAt,
	)
	return err
}

// DeleteProvider removes a provider link.
func (s *Store) DeleteProvider(userDocID, provider string) error {
	_, err := s.DB.Exec(`DELETE FROM providers WHERE user_id = ? AND provider = ?`, userDocID, provider)
	return err
}

// ─── Sync blob operations ─────────────────────────────────────────────────

// GetSyncBlobs returns all sync blobs (state only).
func (s *Store) GetSyncBlobs() ([]SyncBlob, error) {
	rows, err := s.DB.Query(`SELECT kind, content_hash, updated_at FROM sync_blobs ORDER BY kind`)
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

// GetSyncBlob returns a single sync blob by kind.
func (s *Store) GetSyncBlob(kind string) (*SyncBlob, error) {
	b := &SyncBlob{}
	err := s.DB.QueryRow(
		`SELECT kind, content_hash, payload, created_at, updated_at FROM sync_blobs WHERE kind = ?`, kind,
	).Scan(&b.Kind, &b.ContentHash, &b.Payload, &b.CreatedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

// UpsertSyncBlob inserts or updates a sync blob.
func (s *Store) UpsertSyncBlob(kind, contentHash, payload string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.DB.Exec(
		`INSERT INTO sync_blobs (kind, content_hash, payload, created_at, updated_at)
		 VALUES (?, ?, ?, datetime('now'), ?)
		 ON CONFLICT(kind) DO UPDATE SET
			content_hash=excluded.content_hash,
			payload=excluded.payload,
			updated_at=?`,
		kind, contentHash, payload, now, now,
	)
	return err
}

// WipeSyncBlobs removes all sync blobs.
func (s *Store) WipeSyncBlobs() error {
	_, err := s.DB.Exec(`DELETE FROM sync_blobs`)
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
