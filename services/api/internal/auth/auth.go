// Package auth implements email magic links, Google OAuth and cookie sessions (SPEC §8.4).
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"pagdandi/api/internal/config"
	"pagdandi/api/internal/httpx"
	"pagdandi/api/internal/store"
)

// CookieName is the session cookie.
const CookieName = "pd_session"

// SessionTTL is how long a session lasts.
const SessionTTL = 30 * 24 * time.Hour

// MagicLinkTTL is how long a sign-in link is valid.
const MagicLinkTTL = 20 * time.Minute

// googleEndpoint is Google's OAuth 2.0 endpoint (avoids importing oauth2/google and its cloud deps).
var googleEndpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// Service wires auth endpoints.
type Service struct {
	Cfg   *config.Config
	DB    *store.DB
	Log   *slog.Logger
	Mail  func(to, subject, body string) error
	oauth *oauth2.Config
}

// New builds the service.
func New(cfg *config.Config, db *store.DB, log *slog.Logger) *Service {
	s := &Service{Cfg: cfg, DB: db, Log: log}
	s.Mail = s.sendMail
	if cfg.GoogleEnabled() {
		s.oauth = &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			Endpoint:     googleEndpoint,
			RedirectURL:  cfg.PublicBaseURL + "/v1/auth/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
		}
	}
	return s
}

// NewToken returns a random URL-safe token.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Hash keys a token with the session secret so a DB leak does not leak live tokens.
func (s *Service) Hash(token string) []byte {
	m := hmac.New(sha256.New, []byte(s.Cfg.SessionSecret))
	m.Write([]byte(token))
	return m.Sum(nil)
}

// Middleware loads the session user (if any) into the request context.
func (s *Service) Middleware() httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie(CookieName); err == nil && c.Value != "" && len(c.Value) < 200 {
				if u, err := s.DB.UserBySession(r.Context(), s.Hash(c.Value)); err == nil {
					r = r.WithContext(httpx.WithUser(r.Context(), u))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Current returns the signed-in user or nil.
func Current(r *http.Request) *store.User {
	u, _ := httpx.User(r.Context()).(*store.User)
	return u
}

// Require returns the user or a 401.
func Require(r *http.Request) (*store.User, error) {
	if u := Current(r); u != nil {
		return u, nil
	}
	return nil, httpx.Err(http.StatusUnauthorized, "UNAUTHENTICATED", "Please sign in first.")
}

// RequireRole returns the user if they hold one of the roles.
func RequireRole(r *http.Request, roles ...string) (*store.User, error) {
	u, err := Require(r)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		if u.Role == role {
			return u, nil
		}
	}
	return nil, httpx.Err(http.StatusForbidden, "FORBIDDEN", "You do not have access to this.")
}

func (s *Service) setSession(w http.ResponseWriter, r *http.Request, userID string) error {
	tok := NewToken()
	if err := s.DB.CreateSession(r.Context(), userID, s.Hash(tok), time.Now().Add(SessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: tok, Path: "/", MaxAge: int(SessionTTL.Seconds()),
		HttpOnly: true, Secure: !s.Cfg.Dev() || strings.HasPrefix(s.Cfg.PublicBaseURL, "https://"), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// safeRedirect keeps post-login redirects on our own web app.
func (s *Service) safeRedirect(path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\\r\n") {
		path = "/"
	}
	return s.Cfg.WebBaseURL + path
}

// MagicLink handles POST /v1/auth/magic-link.
func (s *Service) MagicLink(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Email    string `json:"email"`
		Redirect string `json:"redirect"`
	}
	if err := httpx.DecodeJSON(w, r, &body, 4<<10); err != nil {
		return err
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(body.Email))
	if err != nil || len(addr.Address) > 254 || !strings.Contains(addr.Address, ".") {
		return httpx.Err(http.StatusBadRequest, "BAD_EMAIL", "That email address does not look right.")
	}
	email := strings.ToLower(addr.Address)
	if n, err := s.DB.CountMagicLinksRecent(r.Context(), email, 15*time.Minute); err == nil && n >= 5 {
		return httpx.Err(http.StatusTooManyRequests, "RATE_LIMITED", "Too many sign-in emails. Try again in a few minutes.")
	}
	tok := NewToken()
	if err := s.DB.CreateMagicLink(r.Context(), email, s.Hash(tok), time.Now().Add(MagicLinkTTL)); err != nil {
		return err
	}
	link := fmt.Sprintf("%s/v1/auth/callback?token=%s&redirect=%s", s.Cfg.PublicBaseURL, url.QueryEscape(tok), url.QueryEscape(body.Redirect))
	if s.Cfg.SMTPHost == "" {
		// Dev: print the link instead of mailing it (SPEC §8.4).
		s.Log.Info("magic link (dev: not emailed)", "email", email, "link", link)
	} else if err := s.Mail(email, "Your Pagdandi sign-in link",
		"Tap to sign in to Pagdandi:\n\n"+link+"\n\nThe link works once and expires in 20 minutes. If you did not ask for it, ignore this email.\n"); err != nil {
		s.Log.Error("send magic link", "err", err)
		return httpx.Err(http.StatusBadGateway, "MAIL_FAILED", "We could not send the email. Please try again.")
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"sent": true})
	return nil
}

// Callback handles GET /v1/auth/callback?token=.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) error {
	tok := r.URL.Query().Get("token")
	if tok == "" || len(tok) > 100 {
		return httpx.Err(http.StatusBadRequest, "BAD_TOKEN", "This sign-in link is not valid.")
	}
	email, err := s.DB.ConsumeMagicLink(r.Context(), s.Hash(tok))
	if errors.Is(err, store.ErrNotFound) {
		http.Redirect(w, r, s.safeRedirect("/signin?error=expired"), http.StatusSeeOther)
		return nil
	}
	if err != nil {
		return err
	}
	u, err := s.DB.UpsertUserByEmail(r.Context(), email, "")
	if err != nil {
		return err
	}
	if err := s.setSession(w, r, u.ID); err != nil {
		return err
	}
	http.Redirect(w, r, s.safeRedirect(r.URL.Query().Get("redirect")), http.StatusSeeOther)
	return nil
}

// Logout handles POST /v1/auth/logout.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = s.DB.DeleteSession(r.Context(), s.Hash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// GoogleStart handles GET /v1/auth/google/start.
func (s *Service) GoogleStart(w http.ResponseWriter, r *http.Request) error {
	if s.oauth == nil {
		return httpx.Err(http.StatusNotFound, "GOOGLE_DISABLED", "Google sign-in is not configured.")
	}
	state := NewToken()
	http.SetCookie(w, &http.Cookie{Name: "pd_oauth_state", Value: state + "|" + url.QueryEscape(r.URL.Query().Get("redirect")),
		Path: "/v1/auth/google", MaxAge: 600, HttpOnly: true, Secure: !s.Cfg.Dev(), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusFound)
	return nil
}

// GoogleCallback handles GET /v1/auth/google/callback.
func (s *Service) GoogleCallback(w http.ResponseWriter, r *http.Request) error {
	if s.oauth == nil {
		return httpx.Err(http.StatusNotFound, "GOOGLE_DISABLED", "Google sign-in is not configured.")
	}
	c, err := r.Cookie("pd_oauth_state")
	if err != nil {
		return httpx.Err(http.StatusBadRequest, "BAD_STATE", "Sign-in expired. Please try again.")
	}
	state, redirect, _ := strings.Cut(c.Value, "|")
	if state == "" || !hmac.Equal([]byte(state), []byte(r.URL.Query().Get("state"))) {
		return httpx.Err(http.StatusBadRequest, "BAD_STATE", "Sign-in expired. Please try again.")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tok, err := s.oauth.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "OAUTH_FAILED", "Google sign-in failed.")
	}
	resp, err := s.oauth.Client(ctx, tok).Get("https://openidconnect.googleapis.com/v1/userinfo")
	if err != nil {
		return httpx.Err(http.StatusBadGateway, "OAUTH_FAILED", "Google sign-in failed.")
	}
	defer resp.Body.Close()
	var info struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"given_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&info); err != nil || info.Email == "" || !info.EmailVerified {
		return httpx.Err(http.StatusBadGateway, "OAUTH_FAILED", "Google did not share a verified email.")
	}
	u, err := s.DB.UpsertUserByEmail(r.Context(), strings.ToLower(info.Email), info.Name)
	if err != nil {
		return err
	}
	if err := s.setSession(w, r, u.ID); err != nil {
		return err
	}
	red, _ := url.QueryUnescape(redirect)
	http.Redirect(w, r, s.safeRedirect(red), http.StatusSeeOther)
	return nil
}

func (s *Service) sendMail(to, subject, body string) error {
	from, err := mail.ParseAddress(s.Cfg.SMTPFrom)
	if err != nil {
		return err
	}
	msg := "From: " + from.String() + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body
	addr := fmt.Sprintf("%s:%d", s.Cfg.SMTPHost, s.Cfg.SMTPPort)
	var a smtp.Auth
	if s.Cfg.SMTPUser != "" {
		a = smtp.PlainAuth("", s.Cfg.SMTPUser, s.Cfg.SMTPPass, s.Cfg.SMTPHost)
	}
	return smtp.SendMail(addr, a, from.Address, []string{to}, []byte(msg))
}
