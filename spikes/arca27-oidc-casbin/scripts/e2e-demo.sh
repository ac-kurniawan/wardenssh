#!/usr/bin/env bash
# ARCA-27 spike — headless end-to-end acceptance test.
#
# Drives the full OIDC authorization-code + PKCE flow against the dockerized
# Zitadel (v4.17.2) and the spike server, without a browser: Zitadel's Session
# API (authenticated with the bootstrap `login-client` PAT) stands in for the
# Login UI, exactly as the v2 Login UI itself does.
#
# Proves the acceptance criteria:
#   1. authorization-code + PKCE login flow completes (real code exchange)
#   2. viewer can GET but cannot POST (403 from Casbin middleware)
#   3. admin can POST (role granted via Casbin)
#   4. token refresh works (new expiry returned)
#   5. logout revokes the session (API returns 401 afterwards)
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SPIKE="$HERE/.."
DEPLOY="$SPIKE/deploy"
APP="${APP_BASE_URL:-http://localhost:7777}"
ISSUER="${OIDC_ISSUER:-http://localhost:8888}"

PAT="$(tr -d '[:space:]' < "$DEPLOY/bootstrap/pat")"
LCPAT="$(tr -d '[:space:]' < "$DEPLOY/bootstrap/login-client.pat")"

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "PASS: $1"; }
bad()  { FAIL=$((FAIL+1)); echo "FAIL: $1"; }
assert_status() { # name expected actual
  if [[ "$3" == "$2" ]]; then ok "$1 (HTTP $3)"; else bad "$1 — expected $2, got $3"; fi
}

# login_as USER -> prints "state<TAB>callbackUrl" after creating a session
login_as() {
  local user="$1"
  # 1. spike server /login -> 302 to authorize URL (carries state + PKCE challenge)
  local authz_url
  authz_url="$(curl -s -o /dev/null -D - "$APP/login" | tr -d '\r' | grep -i '^location:' | sed 's/^location: //I')"
  # 2. authorize -> 302 to the (unused) v2 login UI carrying the authRequest id
  local red auth_req
  red="$(curl -s -o /dev/null -w '%{redirect_url}' "$authz_url")"
  auth_req="$(python3 -c "import sys,urllib.parse as up; q=dict(up.parse_qsl(up.urlparse(sys.argv[1]).query)); print(q.get('authRequest',''))" "$red")"
  [[ -n "$auth_req" ]] || { echo "no authRequest id from authorize redirect" >&2; return 1; }
  # 3. Session API: user+password checks (login-client PAT = what the v2 Login UI uses)
  local sess sid stoken
  sess="$(curl -s -X POST -H "Authorization: Bearer $LCPAT" -H "Content-Type: application/json" \
    -d "{\"checks\":{\"user\":{\"loginName\":\"$user\"},\"password\":{\"password\":\"Password1!\"}}}" \
    "$ISSUER/v2/sessions")"
  sid="$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['sessionId'])" "$sess")"
  stoken="$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['sessionToken'])" "$sess")"
  # 4. Finalize the auth request with the session -> callback URL with code+state
  curl -s -X POST -H "Authorization: Bearer $LCPAT" -H "Content-Type: application/json" \
    -d "{\"session\":{\"sessionId\":\"$sid\",\"sessionToken\":\"$stoken\"}}" \
    "$ISSUER/v2/oidc/auth_requests/$auth_req" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['callbackUrl'])"
}

complete_login() { # complete_login USER JAR
  local cb; cb="$(login_as "$1")"
  curl -s -o /dev/null -c "$2" -L "$cb"   # follow to /auth/callback; jar stores session cookie
}

post_json() { # post_json JAR METHOD PATH [BODY]
  local jar="$1" method="$2" path="$3" body="${4:-{}}"
  curl -s -o /tmp/e2e-body.json -w "%{http_code}" -b "$jar" -X "$method" \
    -H "Content-Type: application/json" -d "$body" "$APP$path"
}

VIEWER_JAR="$(mktemp)"
ADMIN_JAR="$(mktemp)"
trap 'rm -f "$VIEWER_JAR" "$ADMIN_JAR"' EXIT

echo "=== login flows (authorization code + PKCE) ==="
complete_login "viewer@acme.local" "$VIEWER_JAR" && ok "viewer login completed (code exchanged)" || bad "viewer login"
complete_login "admin@acme.local"  "$ADMIN_JAR"  && ok "admin login completed (code exchanged)"  || bad "admin login"

echo "=== whoami ==="
curl -s -b "$VIEWER_JAR" "$APP/api/me" | python3 -c "import json,sys; d=json.load(sys.stdin); print('viewer roles:', d.get('roles'), 'expiry:', d.get('token_expiry'))"
curl -s -b "$ADMIN_JAR"  "$APP/api/me" | python3 -c "import json,sys; d=json.load(sys.stdin); print('admin roles:', d.get('roles'), 'expiry:', d.get('token_expiry'))"

echo "=== RBAC matrix ==="
assert_status "viewer GET  /api/assets (assets.asset.read)"          200 "$(post_json "$VIEWER_JAR" GET  /api/assets)"
assert_status "viewer POST /api/assets (assets.asset.create)"        403 "$(post_json "$VIEWER_JAR" POST /api/assets '{"code":"X","name":"X"}')"
assert_status "viewer GET  /api/accounting/journal"                  200 "$(post_json "$VIEWER_JAR" GET  /api/accounting/journal)"
assert_status "viewer POST /api/accounting/journal (the PRD 403)"    403 "$(post_json "$VIEWER_JAR" POST /api/accounting/journal '{"amount":1}')"
assert_status "viewer POST /api/projects"                            403 "$(post_json "$VIEWER_JAR" POST /api/projects '{"name":"nope"}')"
assert_status "admin  POST /api/assets (role:admin wildcard)"        201 "$(post_json "$ADMIN_JAR"  POST /api/assets '{"code":"AST-9","name":"Spike"}')"
assert_status "admin  POST /api/accounting/journal"                  201 "$(post_json "$ADMIN_JAR"  POST /api/accounting/journal '{"lines":[]}')"
assert_status "unauthenticated GET /api/assets"                      401 "$(post_json /dev/null GET /api/assets)"

echo "=== 403 response body (viewer POST /api/assets) ==="
post_json "$VIEWER_JAR" POST /api/assets '{"code":"X"}' >/dev/null
python3 -m json.tool /tmp/e2e-body.json

echo "=== refresh ==="
curl -s -b "$VIEWER_JAR" -X POST "$APP/auth/refresh" > /tmp/e2e-refresh.json
python3 - <<'EOF'
import json, sys
d = json.load(open('/tmp/e2e-refresh.json'))
print("refreshed:", d.get("refreshed"), "| old:", d.get("old_expiry"), "| new:", d.get("new_expiry"))
sys.exit(0 if d.get("refreshed") and d.get("new_expiry") else 1)
EOF
if [[ $? -eq 0 ]]; then ok "token refresh (new expiry issued)"; else bad "token refresh"; fi

echo "=== logout ==="
assert_status "logout revokes + clears session" 200 "$(post_json "$VIEWER_JAR" POST /logout)"
assert_status "post-logout GET /api/assets -> 401" 401 "$(post_json "$VIEWER_JAR" GET /api/assets)"

echo
echo "=== RESULT: $PASS passed, $FAIL failed ==="
[[ $FAIL -eq 0 ]]
