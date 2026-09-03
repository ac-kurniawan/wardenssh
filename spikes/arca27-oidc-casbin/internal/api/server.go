// Package api assembles the spike HTTP server: OIDC login (authorization code
// + PKCE), cookie sessions, refresh, logout, and the sample RBAC-guarded
// module routes.
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/auth"
	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/authz"
	"github.com/ac-kurniawan/wardenssh/spikes/arca27-oidc-casbin/internal/config"
)

// Server is the spike HTTP server.
type Server struct {
	cfg      *config.Config
	auth     *auth.Manager
	enforcer *authz.Enforcer
	log      *slog.Logger

	mu       sync.Mutex
	sessions map[string]*auth.Session // session id -> session
	pending  map[string]*pendingLogin // state -> PKCE verifier (10 min TTL)
}

type pendingLogin struct {
	verifier  string
	createdAt time.Time
}

// New builds the server.
func New(cfg *config.Config, m *auth.Manager, en *authz.Enforcer, log *slog.Logger) *Server {
	return &Server{
		cfg:      cfg,
		auth:     m,
		enforcer: en,
		log:      log,
		sessions: map[string]*auth.Session{},
		pending:  map[string]*pendingLogin{},
	}
}

// Handler returns the routed http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.handleCallback)
	mux.HandleFunc("GET /{$}", s.handleIndex)

	// Session management
	mux.Handle("GET /api/me", s.requireSession(http.HandlerFunc(s.handleMe)))
	mux.Handle("POST /auth/refresh", s.requireSession(http.HandlerFunc(s.handleRefresh)))
	mux.HandleFunc("POST /logout", s.handleLogout)

	// Sample module routes (platform slice stand-ins)
	mux.Handle("GET /api/projects", s.guard(authz.PermPlatformRead, http.HandlerFunc(s.handleProjectsList)))
	mux.Handle("POST /api/projects", s.guard(authz.PermPlatformCreate, http.HandlerFunc(s.handleProjectsCreate)))
	mux.Handle("GET /api/assets", s.guard(authz.PermAssetRead, http.HandlerFunc(s.handleAssetsList)))
	mux.Handle("POST /api/assets", s.guard(authz.PermAssetCreate, http.HandlerFunc(s.handleAssetsCreate)))
	mux.Handle("GET /api/accounting/journal", s.guard(authz.PermJournalRead, http.HandlerFunc(s.handleJournalList)))
	mux.Handle("POST /api/accounting/journal", s.guard(authz.PermJournalPost, http.HandlerFunc(s.handleJournalPost)))

	return s.logRequests(mux)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).Round(time.Millisecond))
	})
}

// --- auth flow handlers ---

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state := randomToken(16)
	url, verifier := s.auth.AuthCodeURL(state)
	s.mu.Lock()
	// opportunistic cleanup of stale pending logins
	for st, p := range s.pending {
		if time.Since(p.createdAt) > 10*time.Minute {
			delete(s.pending, st)
		}
	}
	s.pending[state] = &pendingLogin{verifier: verifier, createdAt: time.Now()}
	s.mu.Unlock()
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing state or code"})
		return
	}
	s.mu.Lock()
	p, ok := s.pending[state]
	if ok {
		delete(s.pending, state)
	}
	s.mu.Unlock()
	if !ok || time.Since(p.createdAt) > 10*time.Minute {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown or expired state"})
		return
	}

	tok, err := s.auth.Exchange(r.Context(), code, p.verifier)
	if err != nil {
		s.log.Error("token exchange failed", "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "token exchange failed"})
		return
	}
	sess, err := s.auth.BuildSession(r.Context(), tok)
	if err != nil {
		s.log.Error("session build failed", "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token set"})
		return
	}
	sid := randomToken(32)
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.OIDC.CookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sid, ok := s.currentSessionID(r); ok {
		s.mu.Lock()
		sess := s.sessions[sid]
		delete(s.sessions, sid)
		s.mu.Unlock()
		if sess != nil {
			s.auth.Revoke(r.Context(), sess.RefreshToken)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.OIDC.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.session(r)
	oldExpiry := sess.Expiry
	tok, err := s.auth.Refresh(r.Context(), sess.RefreshToken)
	if err != nil {
		s.log.Error("refresh failed", "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "refresh failed"})
		return
	}
	sess.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		sess.RefreshToken = tok.RefreshToken
	}
	sess.Expiry = tok.Expiry
	writeJSON(w, http.StatusOK, map[string]any{
		"refreshed":  true,
		"old_expiry": oldExpiry.UTC().Format(time.RFC3339),
		"new_expiry": sess.Expiry.UTC().Format(time.RFC3339),
	})
}

// --- module sample handlers ---

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.session(r)
	rawClaims, rawErr := auth.JSONClaims(sess.IDToken)
	if rawErr != nil {
		rawClaims = "{}"
	}
	// Embedded JSON diagnostics: emit claims as a raw JSON object, not a string.
	var claimsAny any
	_ = json.Unmarshal([]byte(rawClaims), &claimsAny)
	if claimsAny == nil {
		claimsAny = map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"subject":      sess.Subject,
		"login_name":   sess.LoginName,
		"display_name": sess.DisplayName,
		"roles":        sess.Roles,
		"token_expiry": sess.Expiry.UTC().Format(time.RFC3339),
		"id_claims":    claimsAny,
	})
}

func (s *Server) handleProjectsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]string{
			{"code": "PRJ-001", "name": "Platform"},
			{"code": "PRJ-002", "name": "Assets"},
		},
	})
}

func (s *Server) handleProjectsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusCreated, map[string]string{"name": body.Name, "status": "created"})
}

func (s *Server) handleAssetsList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]string{
			{"code": "AST-001", "name": "Forklift 1", "class": "equipment"},
			{"code": "AST-002", "name": "MacBook Pro 47", "class": "it"},
		},
	})
}

func (s *Server) handleAssetsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusCreated, map[string]string{"code": body.Code, "name": body.Name, "status": "created"})
}

func (s *Server) handleJournalList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]string{}})
}

func (s *Server) handleJournalPost(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusCreated, map[string]any{"status": "posted", "entry": body})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentSessionID(r); !ok {
		writeHTML(w, http.StatusOK, indexLoggedOutHTML)
		return
	}
	writeHTML(w, http.StatusOK, indexLoggedInHTML)
}

// --- middleware ---

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.session(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) guard(perm string, next http.Handler) http.Handler {
	// Chain (outermost first): authenticate+attach identity -> authorize -> handler.
	permChecked := s.enforcer.RequirePermission(perm, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.session(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		permChecked.ServeHTTP(w, authz.WithIdentity(r, &authz.Identity{Subject: sess.Subject, Roles: sess.Roles}))
	})
}

// --- session helpers ---

func (s *Server) currentSessionID(r *http.Request) (string, bool) {
	c, err := r.Cookie(s.cfg.OIDC.CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[c.Value]; !ok {
		return "", false
	}
	return c.Value, true
}

func (s *Server) session(r *http.Request) (*auth.Session, bool) {
	sid, ok := s.currentSessionID(r)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sid]
	return sess, ok
}

// Shutdown clears all state (used by tests).
func (s *Server) Shutdown(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]*auth.Session{}
	s.pending = map[string]*pendingLogin{}
	return nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
