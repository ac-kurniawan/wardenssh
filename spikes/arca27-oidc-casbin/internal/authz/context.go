package authz

import (
	"context"
	"net/http"
)

// contextWithValue attaches the identity to the request context.
func contextWithValue(r *http.Request, id *Identity) context.Context {
	return context.WithValue(r.Context(), identityKey, id)
}
