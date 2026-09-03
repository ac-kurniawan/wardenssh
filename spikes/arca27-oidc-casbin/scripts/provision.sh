#!/usr/bin/env bash
# Provision Zitadel dev instance for the ARCA-27 spike:
#   project + roles + PKCE web app + test users + role grants.
# Requires: the Zitadel container running (docker compose up -d) with the
# bootstrap PAT at deploy/bootstrap/pat (written by start-from-init).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$HERE/../deploy"
ENV_FILE="$DEPLOY_DIR/.env.local"

PORT="${ZITADEL_PORT:-8888}"
ISSUER="http://localhost:${PORT}"
REDIRECT_URI="${APP_BASE_URL:-http://localhost:7777}/auth/callback"
POST_LOGOUT="${APP_BASE_URL:-http://localhost:7777}/"

if [[ ! -f "$DEPLOY_DIR/bootstrap/pat" ]]; then
  echo "ERROR: $DEPLOY_DIR/bootstrap/pat not found." >&2
  echo "Start the stack first: docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d" >&2
  exit 1
fi
PAT="$(tr -d '[:space:]' < "$DEPLOY_DIR/bootstrap/pat")"
ORG_ID=""

api() { # api METHOD PATH [JSON]
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -s -X "$method" -H "Authorization: Bearer $PAT" -H "Content-Type: application/json" \
      ${ORG_ID:+-H "x-zitadel-orgid: $ORG_ID"} -d "$body" "$ISSUER$path"
  else
    curl -s -H "Authorization: Bearer $PAT" ${ORG_ID:+-H "x-zitadel-orgid: $ORG_ID"} "$ISSUER$path"
  fi
}

json_field() { python3 -c "import json,sys; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1" 2>/dev/null; }

# 1. Resolve the default organisation id
ORG_ID="$(api GET /management/v1/orgs/me | json_field "d['org']['id']")"
echo "org:    $ORG_ID"

# 2. Project (roles asserted into tokens)
PROJECT_ID="$(api POST /management/v1/projects \
  '{"name":"erp-dev","projectRoleAssertion":true,"projectRoleCheck":false}' | json_field "d['id']")"
echo "project: $PROJECT_ID"

# 3. Roles (the four PRD roles)
for spec in 'admin:Administrator' 'accountant:Accountant' 'asset-manager:Asset Manager' 'viewer:Viewer'; do
  key="${spec%%:*}"; label="${spec#*:}"
  api POST "/management/v1/projects/$PROJECT_ID/roles" \
    "{\"roleKey\":\"$key\",\"displayName\":\"$label\",\"group\":\"erp\"}" >/dev/null
done
echo "roles:  admin accountant asset-manager viewer"

# 4. OIDC app — web app, authorization code + PKCE, refresh tokens, JWT access tokens
APP_JSON="$(api POST "/management/v1/projects/$PROJECT_ID/apps/oidc" '{
  "name":"erp-spike",
  "redirectUris":["'"$REDIRECT_URI"'"],
  "postLogoutRedirectUris":["'"$POST_LOGOUT"'"],
  "responseTypes":["OIDC_RESPONSE_TYPE_CODE"],
  "grantTypes":["OIDC_GRANT_TYPE_AUTHORIZATION_CODE","OIDC_GRANT_TYPE_REFRESH_TOKEN"],
  "appType":"OIDC_APP_TYPE_WEB",
  "authMethodType":"OIDC_AUTH_METHOD_TYPE_NONE",
  "accessTokenType":"OIDC_TOKEN_TYPE_JWT",
  "idTokenRoleAssertion":true,
  "accessTokenRoleAssertion":true,
  "skipNativeAppSuccessPage":true
}')"
CLIENT_ID="$(echo "$APP_JSON" | json_field "d['clientId']")"
echo "client: $CLIENT_ID"

# 5. Test users (admin + viewer), verified emails + working passwords, via the
# v2 API (the legacy /management/v1/users/human endpoint leaves users in
# USER_STATE_INITIAL with no usable password).
mkuser() { # mkuser USERNAME FIRSTNAME LASTNAME
  api POST /v2/users/human '{
    "userName":"'"$1"'",
    "profile":{"givenName":"'"$2"'","familyName":"'"$3"'"},
    "email":{"email":"'"$1"'","isVerified":true},
    "password":{"password":"Password1!","changeRequired":false}
  }' | json_field "d['userId']"
}
ADMIN_ID="$(mkuser admin@acme.local Ada Admin)"
VIEWER_ID="$(mkuser viewer@acme.local Vera Viewer)"
echo "users:  $ADMIN_ID (admin) / $VIEWER_ID (viewer)"

# 6. Assign user grants (role per user)
api POST "/management/v1/users/$ADMIN_ID/grants" \
  "{\"projectId\":\"$PROJECT_ID\",\"roleKeys\":[\"admin\"]}" >/dev/null
api POST "/management/v1/users/$VIEWER_ID/grants" \
  "{\"projectId\":\"$PROJECT_ID\",\"roleKeys\":[\"viewer\"]}" >/dev/null
echo "grants: admin->admin, viewer->viewer"

# 7. Emit env file consumed by the Go server
cat > "$ENV_FILE" <<EOF
OIDC_ISSUER=$ISSUER
OIDC_CLIENT_ID=$CLIENT_ID
OIDC_PROJECT_ID=$PROJECT_ID
OIDC_REDIRECT_URI=$REDIRECT_URI
APP_SECRET=$(openssl rand -hex 32)
EOF
echo "wrote:  $ENV_FILE"
