// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string

	ListenAddr     string
	AllowedOrigins []string
	CookieSecure   bool
	SessionTTL     time.Duration
	SessionSecret  []byte

	AuthRatePerMinute float64
	AuthRateBurst     int

	GitHub GitHubConfig
}

type GitHubConfig struct {
	AppID          int64
	InstallationID int64
	PrivateKeyPEM  []byte
	APIBase        string // default https://api.github.com
	GraphQLURL     string // default https://api.github.com/graphql
}

// Configured reports whether enough GitHub settings are present to publish issues.
func (g GitHubConfig) Configured() bool {
	return g.AppID != 0 && g.InstallationID != 0 && len(g.PrivateKeyPEM) > 0
}

// Load reads configuration from the environment, applying defaults. It returns an
// error only for values that are present but malformed; missing GitHub settings are
// tolerated so DB-only tooling (admin CLI) can run without them.
func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:       getenv("DATABASE_URL", "postgres://idea:idea@localhost:5432/idea?sslmode=disable"),
		ListenAddr:        getenv("LISTEN_ADDR", ":8080"),
		AllowedOrigins:    splitAndTrim(getenv("ALLOWED_ORIGINS", "")),
		CookieSecure:      getenvBool("COOKIE_SECURE", false),
		SessionTTL:        getenvDuration("SESSION_TTL", 2*time.Hour),
		SessionSecret:     []byte(getenv("SESSION_SECRET", "")),
		AuthRatePerMinute: getenvFloat("AUTH_RATE_PER_MINUTE", 10),
		AuthRateBurst:     getenvInt("AUTH_RATE_BURST", 5),
	}

	gh := GitHubConfig{
		AppID:          getenvInt64("GITHUB_APP_ID", 0),
		InstallationID: getenvInt64("GITHUB_APP_INSTALLATION_ID", 0),
		APIBase:        strings.TrimRight(getenv("GITHUB_API_BASE", "https://api.github.com"), "/"),
		GraphQLURL:     getenv("GITHUB_GRAPHQL_URL", "https://api.github.com/graphql"),
	}
	if keyFile := getenv("GITHUB_APP_PRIVATE_KEY_FILE", ""); keyFile != "" {
		pem, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("reading GITHUB_APP_PRIVATE_KEY_FILE: %w", err)
		}
		gh.PrivateKeyPEM = pem
	}
	c.GitHub = gh

	return c, nil
}

// RequireServer validates settings that the HTTP server cannot run without.
func (c *Config) RequireServer() error {
	if len(c.SessionSecret) < 16 {
		return fmt.Errorf("SESSION_SECRET must be at least 16 bytes")
	}
	if !c.GitHub.Configured() {
		return fmt.Errorf("GitHub App not configured (need GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, GITHUB_APP_PRIVATE_KEY_FILE)")
	}
	return nil
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getenvBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return b
		}
	}
	return def
}

func getenvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func getenvInt64(key string, def int64) int64 {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
}
