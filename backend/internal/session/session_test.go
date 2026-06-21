package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newReqWithCookie(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: value})
	return r
}

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	m := NewManager([]byte("test-secret-at-least-16-bytes"), time.Hour, true)
	rec := httptest.NewRecorder()
	m.Issue(rec, "ABCD-1234-WXYZ", "proj-1", "Jane Doe")

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie missing security attributes: %+v", c)
	}

	got, err := m.Verify(newReqWithCookie(c.Value))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.AuthCode != "ABCD-1234-WXYZ" || got.ProjectID != "proj-1" || got.DisplayName != "Jane Doe" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	m := NewManager([]byte("test-secret-at-least-16-bytes"), time.Hour, false)
	rec := httptest.NewRecorder()
	m.Issue(rec, "code", "proj", "name")
	token := rec.Result().Cookies()[0].Value

	// Flip the last character of the signature.
	tampered := token[:len(token)-1] + flip(token[len(token)-1])
	if _, err := m.Verify(newReqWithCookie(tampered)); err != ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestVerifyRejectsForeignSecret(t *testing.T) {
	issuer := NewManager([]byte("secret-one-at-least-16b"), time.Hour, false)
	attacker := NewManager([]byte("secret-two-at-least-16b"), time.Hour, false)
	rec := httptest.NewRecorder()
	issuer.Issue(rec, "code", "proj", "name")
	token := rec.Result().Cookies()[0].Value

	if _, err := attacker.Verify(newReqWithCookie(token)); err != ErrInvalid {
		t.Errorf("expected ErrInvalid for foreign secret, got %v", err)
	}
}

func TestVerifyExpired(t *testing.T) {
	m := NewManager([]byte("test-secret-at-least-16-bytes"), time.Hour, false)
	base := time.Now()
	m.now = func() time.Time { return base }
	rec := httptest.NewRecorder()
	m.Issue(rec, "code", "proj", "name")
	token := rec.Result().Cookies()[0].Value

	m.now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, err := m.Verify(newReqWithCookie(token)); err != ErrExpired {
		t.Errorf("expected ErrExpired, got %v", err)
	}
}

func TestVerifyNoCookie(t *testing.T) {
	m := NewManager([]byte("test-secret-at-least-16-bytes"), time.Hour, false)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := m.Verify(r); err != ErrNoCookie {
		t.Errorf("expected ErrNoCookie, got %v", err)
	}
}

func flip(b byte) string {
	if b == 'A' {
		return "B"
	}
	return "A"
}
