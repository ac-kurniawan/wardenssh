// Package authz wires Casbin RBAC (module.resource.action permissions) into
// net/http middleware. Roles arrive from OIDC role claims; binding token
// subjects to Casbin roles happens lazily per request (in-memory, spike-only).
package authz

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/casbin/casbin/v2"
)

// Permission strings used by the spike routes (module.resource.action).
const (
	PermPlatformRead   = "platform.projects.read"
	PermPlatformCreate = "platform.projects.create"
	PermAssetRead      = "assets.asset.read"
	PermAssetCreate    = "assets.asset.create"
	PermJournalRead    = "accounting.journal.read"
	PermJournalPost    = "accounting.journal.post"
)

// Identity is the authenticated principal attached to a request.
type Identity struct {
	Subject string
	Roles   []string
}

type contextKey string

const identityKey contextKey = "arca27.identity"

// Enforcer wraps the Casbin enforcer with lazy role binding.
type Enforcer struct {
	e     *casbin.Enforcer
	mu    sync.Mutex
	bound map[string]bool
}

// NewEnforcer loads the Casbin model and policy files.
func NewEnforcer(modelPath, policyPath string) (*Enforcer, error) {
	e, err := casbin.NewEnforcer(modelPath, policyPath)
	if err != nil {
		return nil, fmt.Errorf("casbin init: %w", err)
	}
	e.EnableAutoSave(false) // spike: keep policy file pristine, bindings live in memory
	return &Enforcer{e: e, bound: map[string]bool{}}, nil
}

// EnsureBinding assigns roles to a subject (idempotent, in-memory).
func (en *Enforcer) EnsureBinding(subject string, roles []string) {
	en.mu.Lock()
	defer en.mu.Unlock()
	if en.bound[subject] {
		return
	}
	for _, role := range roles {
		if role == "" {
			continue
		}
		_, _ = en.e.AddRoleForUser(subject, role)
	}
	en.bound[subject] = true
}

// Enforce checks whether the subject holds the permission.
// Permission format: module.resource.action (e.g. assets.asset.read).
func (en *Enforcer) Enforce(subject, perm string) (bool, error) {
	return en.e.Enforce(subject, perm)
}

// IdentityFromContext returns the request identity, if any.
func IdentityFromContext(r *http.Request) (*Identity, bool) {
	id, ok := r.Context().Value(identityKey).(*Identity)
	return id, ok && id != nil
}

// WithIdentity stores the identity on the request context.
func WithIdentity(r *http.Request, id *Identity) *http.Request {
	ctx := contextWithValue(r, id)
	return r.WithContext(ctx)
}

// RequirePermission is the authz middleware: 401 when unauthenticated,
// 403 when the identity lacks the requested permission. The identity must
// already be attached to the request context (see WithIdentity).
func (en *Enforcer) RequirePermission(perm string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFromContext(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthenticated"})
			return
		}
		en.EnsureBinding(id.Subject, id.Roles)
		allowed, err := en.Enforce(id.Subject, perm)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "authz failure"})
			return
		}
		if !allowed {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error":    "forbidden",
				"required": perm,
				"subject":  id.Subject,
				"roles":    id.Roles,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		_, _ = w.Write([]byte(`{"error":"encoding failure"}`))
	}
}
