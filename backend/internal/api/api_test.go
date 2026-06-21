package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jasonross/idea-collect/backend/internal/ratelimit"
	"github.com/jasonross/idea-collect/backend/internal/session"
)

func testServer(t *testing.T, perMinute float64, burst int) *Server {
	t.Helper()
	sm := session.NewManager([]byte("test-secret-at-least-16-bytes"), time.Hour, false)
	rl := ratelimit.New(perMinute, burst)
	t.Cleanup(rl.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// store and publisher are nil: the paths under test return before touching them.
	return NewServer(nil, sm, rl, nil, []string{"http://localhost:5173"}, log)
}

func TestComposeIssueBody(t *testing.T) {
	got := composeIssueBody("Jane Doe", "Add dark mode")
	if !strings.Contains(got, "**Submitted by:** Jane Doe") || !strings.Contains(got, "Add dark mode") {
		t.Errorf("unexpected body: %q", got)
	}
}

func TestClientIPPrefersForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:5000"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if ip := clientIP(r); ip != "203.0.113.7" {
		t.Errorf("got %q, want 203.0.113.7", ip)
	}
}

func TestAuthEmptyCodeIsBadRequest(t *testing.T) {
	s := testServer(t, 100, 100)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(`{"auth_code":"   "}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
}

func TestAuthRateLimited(t *testing.T) {
	s := testServer(t, 1, 2) // burst of 2
	do := func() int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth", strings.NewReader(`{"auth_code":""}`))
		req.RemoteAddr = "198.51.100.5:1234"
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	_ = do()
	_ = do()
	if code := do(); code != http.StatusTooManyRequests {
		t.Errorf("third attempt: got %d, want 429", code)
	}
}

func TestSubmitWithoutSessionUnauthorized(t *testing.T) {
	s := testServer(t, 100, 100)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/submissions", strings.NewReader(`{"title":"t","body":"b"}`))
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	s := testServer(t, 100, 100)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/auth", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight got %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("missing CORS allow-origin header")
	}
}
