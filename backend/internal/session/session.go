// Package session issues and verifies stateless, HMAC-signed session cookies.
// A cookie carries the authenticated auth_code + project so the SPA can submit
// multiple ideas after entering its code once.
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const CookieName = "idc_session"

var (
	ErrNoCookie = errors.New("no session cookie")
	ErrInvalid  = errors.New("invalid session cookie")
	ErrExpired  = errors.New("session expired")
)

// Session is the data carried inside the signed cookie.
type Session struct {
	AuthCode    string `json:"c"`
	ProjectID   string `json:"p"`
	DisplayName string `json:"n"`
	ExpiresAt   int64  `json:"e"` // unix seconds
}

type Manager struct {
	secret []byte
	ttl    time.Duration
	secure bool
	now    func() time.Time // overridable in tests
}

func NewManager(secret []byte, ttl time.Duration, secure bool) *Manager {
	return &Manager{secret: secret, ttl: ttl, secure: secure, now: time.Now}
}

// Issue signs a session for the given identity and writes it as a cookie.
func (m *Manager) Issue(w http.ResponseWriter, authCode, projectID, displayName string) {
	exp := m.now().Add(m.ttl)
	s := Session{AuthCode: authCode, ProjectID: projectID, DisplayName: displayName, ExpiresAt: exp.Unix()}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    m.encode(s),
		Path:     "/",
		Expires:  exp,
		MaxAge:   int(m.ttl.Seconds()),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// Clear expires the session cookie.
func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// Verify reads and validates the session cookie from the request.
func (m *Manager) Verify(r *http.Request) (Session, error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, ErrNoCookie
	}
	return m.decode(c.Value)
}

func (m *Manager) encode(s Session) string {
	payload, _ := json.Marshal(s)
	b64 := base64.RawURLEncoding.EncodeToString(payload)
	return b64 + "." + base64.RawURLEncoding.EncodeToString(m.mac([]byte(b64)))
}

func (m *Manager) decode(token string) (Session, error) {
	b64, sig, ok := strings.Cut(token, ".")
	if !ok {
		return Session{}, ErrInvalid
	}
	gotMac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(gotMac, m.mac([]byte(b64))) {
		return Session{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return Session{}, ErrInvalid
	}
	var s Session
	if err := json.Unmarshal(payload, &s); err != nil {
		return Session{}, ErrInvalid
	}
	if m.now().Unix() >= s.ExpiresAt {
		return Session{}, ErrExpired
	}
	return s, nil
}

func (m *Manager) mac(b []byte) []byte {
	h := hmac.New(sha256.New, m.secret)
	h.Write(b)
	return h.Sum(nil)
}
