// Package api wires the HTTP handlers for the public submission flow.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jasonross/idea-collect/backend/internal/authcode"
	"github.com/jasonross/idea-collect/backend/internal/github"
	"github.com/jasonross/idea-collect/backend/internal/ratelimit"
	"github.com/jasonross/idea-collect/backend/internal/session"
	"github.com/jasonross/idea-collect/backend/internal/store"
)

const (
	maxTitleLen = 200
	maxBodyLen  = 5000
)

// Publisher creates a GitHub issue and adds it to a project board.
type Publisher interface {
	CreateIssueOnBoard(ctx context.Context, owner, repo, projectNodeID, title, body string) (github.Result, error)
}

type Server struct {
	store     *store.Store
	sessions  *session.Manager
	limiter   *ratelimit.Limiter
	publisher Publisher
	origins   map[string]bool
	log       *slog.Logger
}

func NewServer(st *store.Store, sm *session.Manager, rl *ratelimit.Limiter, pub Publisher, allowedOrigins []string, log *slog.Logger) *Server {
	origins := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		origins[o] = true
	}
	return &Server{store: st, sessions: sm, limiter: rl, publisher: pub, origins: origins, log: log}
}

// Handler returns the fully-wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/auth", s.handleAuth)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST /api/submissions", s.handleSubmit)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	return s.recoverer(s.cors(s.requestLog(mux)))
}

// ---- handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type authRequest struct {
	AuthCode string `json:"auth_code"`
}
type authResponse struct {
	ProjectName string `json:"project_name"`
	DisplayName string `json:"display_name"`
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.Allow(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, please wait and try again")
		return
	}

	var req authRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	code := authcode.Normalize(req.AuthCode)
	if code == "" {
		writeError(w, http.StatusBadRequest, "please enter your invite code")
		return
	}

	user, project, err := s.store.GetActiveUser(r.Context(), code)
	if err != nil {
		// fail2ban-parseable line keyed on the real client IP.
		s.log.Warn("auth failure", "event", "auth_failed", "ip", ip)
		writeError(w, http.StatusUnauthorized, "that invite code is not valid")
		return
	}

	s.sessions.Issue(w, user.AuthCode, project.ID, user.DisplayName)
	writeJSON(w, http.StatusOK, authResponse{ProjectName: project.Name, DisplayName: user.DisplayName})
}

type sessionResponse struct {
	ProjectName string `json:"project_name"`
	DisplayName string `json:"display_name"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessions.Verify(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	project, err := s.store.GetProjectByID(r.Context(), sess.ProjectID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{ProjectName: project.Name, DisplayName: sess.DisplayName})
}

type submitRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
type submitResponse struct {
	IssueURL    string `json:"issue_url"`
	IssueNumber int    `json:"issue_number"`
}

func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sessions.Verify(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "please enter your invite code first")
		return
	}

	var req submitRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	if title == "" || body == "" {
		writeError(w, http.StatusBadRequest, "please fill in both a title and a description")
		return
	}
	if len(title) > maxTitleLen || len(body) > maxBodyLen {
		writeError(w, http.StatusBadRequest, "your idea is too long")
		return
	}

	project, err := s.store.GetProjectByID(r.Context(), sess.ProjectID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "please enter your invite code first")
		return
	}

	subID, err := s.store.CreateSubmission(r.Context(), sess.AuthCode, project.ID, title, body)
	if err != nil {
		s.log.Error("create submission", "err", err)
		writeError(w, http.StatusInternalServerError, "could not save your idea, please try again")
		return
	}

	issueBody := composeIssueBody(sess.DisplayName, body)
	res, err := s.publisher.CreateIssueOnBoard(r.Context(),
		project.GitHubOwner, project.GitHubRepo, project.GitHubProjectID, title, issueBody)
	if err != nil {
		_ = s.store.MarkSubmissionFailed(r.Context(), subID, err.Error())
		s.log.Error("publish submission", "id", subID, "err", err)
		writeError(w, http.StatusBadGateway, "we saved your idea but couldn't post it yet; we'll retry shortly")
		return
	}

	if err := s.store.MarkSubmissionCreated(r.Context(), subID, res.IssueNumber, res.IssueURL, res.IssueNodeID); err != nil {
		s.log.Error("mark submission created", "id", subID, "err", err)
	}
	writeJSON(w, http.StatusCreated, submitResponse{IssueURL: res.IssueURL, IssueNumber: res.IssueNumber})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

// composeIssueBody puts the submitter's name in the issue body.
func composeIssueBody(name, body string) string {
	var b strings.Builder
	b.WriteString("**Submitted by:** ")
	b.WriteString(name)
	b.WriteString("\n\n---\n\n")
	b.WriteString(body)
	return b.String()
}

// ---- small helpers ----

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func clientIP(r *http.Request) string {
	// Backend is only reachable through Caddy on an internal network, so the
	// left-most X-Forwarded-For entry is the real client.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}
