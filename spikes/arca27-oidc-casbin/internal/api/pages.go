package api

import (
	"net/http"
)

const indexLoggedOutHTML = `<!doctype html>
<html>
<head><title>ARCA-27 Spike</title></head>
<body style="font-family: system-ui; max-width: 40rem; margin: 3rem auto;">
  <h1>ARCA-27 — OIDC + Casbin RBAC spike</h1>
  <p>Not logged in.</p>
  <p><a href="/login">Sign in (OIDC, authorization code + PKCE)</a></p>
</body>
</html>`

const indexLoggedInHTML = `<!doctype html>
<html>
<head><title>ARCA-27 Spike</title></head>
<body style="font-family: system-ui; max-width: 40rem; margin: 3rem auto;">
  <h1>ARCA-27 — OIDC + Casbin RBAC spike</h1>
  <p>Logged in.</p>
  <ul>
    <li><a href="/api/me">GET /api/me</a> — identity + roles</li>
    <li>POST /auth/refresh — rotate tokens</li>
    <li>POST /logout — revoke + clear cookie</li>
  </ul>
</body>
</html>`

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
