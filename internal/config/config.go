// Package config loads environment variables for the server.
package config

import (
	"os"
	"strconv"
)

// Config holds all server configuration loaded from environment variables.
type Config struct {
	Port             int
	DatabasePath     string
	DeepLinkScheme   string
	UpstreamAPI      string
	GitHubClientID   string
	GitHubSecret     string
	GoogleClientID   string
	GoogleSecret     string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Port:           envInt("PORT", 3000),
		DatabasePath:   envStr("DATABASE_PATH", "/app/data/synape.db"),
		DeepLinkScheme: envStr("DEEP_LINK_SCHEME", "Synape"),
		UpstreamAPI:    envStr("UPSTREAM_API", ""),
		GitHubClientID: envStr("GITHUB_CLIENT_ID", ""),
		GitHubSecret:   envStr("GITHUB_CLIENT_SECRET", ""),
		GoogleClientID: envStr("GOOGLE_CLIENT_ID", ""),
		GoogleSecret:   envStr("GOOGLE_CLIENT_SECRET", ""),
	}
}

func envStr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
