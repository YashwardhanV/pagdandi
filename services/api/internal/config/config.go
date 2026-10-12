// Package config reads and validates environment configuration (SPEC §8.4).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config is the process configuration.
type Config struct {
	Addr                 string
	DatabaseURL          string
	RedisURL             string
	GHInURL              string
	GHSeURL              string
	PhotonURL            string
	PublicBaseURL        string
	WebBaseURL           string
	CORSOrigins          []string
	SessionSecret        string
	SMTPHost             string
	SMTPPort             int
	SMTPUser             string
	SMTPPass             string
	SMTPFrom             string
	GoogleClientID       string
	GoogleClientSecret   string
	GeocoderContactEmail string
	RegionsFile          string
	Env                  string // dev | prod
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return def
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	c := &Config{
		Addr:                 env("API_ADDR", ":8080"),
		DatabaseURL:          env("DATABASE_URL", "postgres://pagdandi:pagdandi@localhost:5432/pagdandi?sslmode=disable"),
		RedisURL:             env("REDIS_URL", "redis://localhost:6379/0"),
		GHInURL:              env("GH_IN_URL", "http://localhost:8989"),
		GHSeURL:              env("GH_SE_URL", "http://localhost:8990"),
		PhotonURL:            env("PHOTON_URL", "https://photon.komoot.io"),
		PublicBaseURL:        strings.TrimRight(env("PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		WebBaseURL:           strings.TrimRight(env("WEB_BASE_URL", "http://localhost:5173"), "/"),
		SessionSecret:        env("SESSION_SECRET", ""),
		SMTPHost:             env("SMTP_HOST", ""),
		SMTPUser:             env("SMTP_USER", ""),
		SMTPPass:             env("SMTP_PASS", ""),
		SMTPFrom:             env("SMTP_FROM", "Pagdandi <no-reply@localhost>"),
		GoogleClientID:       env("GOOGLE_OAUTH_CLIENT_ID", ""),
		GoogleClientSecret:   env("GOOGLE_OAUTH_CLIENT_SECRET", ""),
		GeocoderContactEmail: env("GEOCODER_CONTACT_EMAIL", ""),
		RegionsFile:          env("REGIONS_FILE", "../../data/regions.yaml"),
		Env:                  env("APP_ENV", "dev"),
	}
	port, err := strconv.Atoi(env("SMTP_PORT", "587"))
	if err != nil {
		return nil, fmt.Errorf("SMTP_PORT: %w", err)
	}
	c.SMTPPort = port
	for _, o := range strings.Split(env("CORS_ORIGINS", c.WebBaseURL), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSOrigins = append(c.CORSOrigins, o)
		}
	}
	return c, c.validate()
}

func (c *Config) validate() error {
	var errs []error
	for k, v := range map[string]string{"PUBLIC_BASE_URL": c.PublicBaseURL, "GH_IN_URL": c.GHInURL, "GH_SE_URL": c.GHSeURL} {
		if _, err := url.ParseRequestURI(v); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", k, err))
		}
	}
	if c.Env == "prod" {
		if len(c.SessionSecret) < 32 {
			errs = append(errs, errors.New("SESSION_SECRET must be at least 32 characters in prod"))
		}
		if !strings.HasPrefix(c.PublicBaseURL, "https://") {
			errs = append(errs, errors.New("PUBLIC_BASE_URL must be https in prod"))
		}
	}
	if c.SessionSecret == "" {
		c.SessionSecret = "dev-insecure-session-secret-change-me-0000"
	}
	return errors.Join(errs...)
}

// Dev reports whether this is a development deployment.
func (c *Config) Dev() bool { return c.Env != "prod" }

// GoogleEnabled reports whether Google OAuth is configured.
func (c *Config) GoogleEnabled() bool { return c.GoogleClientID != "" && c.GoogleClientSecret != "" }
