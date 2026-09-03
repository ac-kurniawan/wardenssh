# ARCA-27 — SPIKE: OIDC + Casbin RBAC plumbing (dockerized Zitadel)

> Throwaway spike, not production code. Proves authn/authz end-to-end for the
> Extensible ERP MVP (PRD: parent issue ARCA-23, F0.4/F0.5) before the platform
> slice (ARCA-29/30) is built.

## Outcome

| Acceptance criterion | Result |
|---|---|
| Dockerized OIDC provider with seeded realm/client/users/roles | ✅ Zitadel v4.17.2, seeded headlessly by `scripts/provision.sh` |
| Authorization-code + PKCE login flow | ✅ real code exchange (server-held verifier) |
| JWT access-token validation via JWKS | ✅ go-oidc remote keyset |
| Token refresh | ✅ `POST /auth/refresh` rotates, new expiry returned |
| Logout | ✅ session cleared + refresh token revoked |
| Casbin RBAC `module.resource.action`, viewer 403 demo | ✅ 13/13 E2E checks green (`scripts/e2e-demo.sh`) |

`RESULT: 13 passed, 0 failed` — see "Verify" below for the exact matrix.

## Provider choice: Zitadel v4.17.2 (not Keycloak)

**Why Zitadel wins for this project:**

1. **Footprint**: single ~150MB container + Postgres, ~2GB RAM total vs Keycloak's
   JVM (~1GB+ idles). Starts to `ready` in <60s locally.
2. **API-first seeding**: instance init can mint an admin machine-user PAT
   (`ZITADEL_FIRSTINSTANCE_PATPATH`), so the entire dev "realm" (org, project,
   roles, PKCE app, users, grants) is a **repeatable script**, not UI clicking.
   Keycloak's equivalent (kcadm.sh) is heavier and its Docker bootstrap story is
   clunkier (no native PAT-at-init equivalent).
3. **JWT access tokens natively** (`OIDC_TOKEN_TYPE_JWT`) — auditable via JWKS
   exactly like ID tokens, which is what the platform middleware wants.
   Keycloak needs a mapper for the same effect.
4. **Role claims built in**: `projectRoleAssertion` emits
   `urn:zitadel:iam:org:project:roles` in ID+access tokens — the namespaced
   permission string can ride from IdP to Casbin with no token mappers.

**Tradeoffs / risks found:**

- v4 ships a separate Login v2 UI (Next.js container). For pure headless dev
  flows the Session API + a `login-client` machine PAT replaces it (that is
  literally what the Login UI calls internally) — see `scripts/e2e-demo.sh`.
  The official compose runs the login container; our compose omits it. For the
  real platform slice with a browser, either add the `zitadel-login` service or
  rely on the API-only flow — decide in ARCA-29.
- Docs restructure mid-v4: some `/docs` paths 404. Source (`cmd/setup/steps.yaml`,
  proto files) is the reliable reference.
- Gotcha: env vars are parsed strictly; time values in env MUST be quoted
  (`"2099-01-01T00:00:00Z"`) or init fails.
- Gotcha: the legacy `POST /management/v1/users/human` accepts a password field
  but leaves users `USER_STATE_INITIAL` without a usable password. Use the v2
  API (`POST /v2/users/human` with `password: {password, changeRequired}`).

## Architecture of the spike

```
spikes/arca27-oidc-casbin/
├── cmd/server/main.go            # wiring: config -> OIDC manager -> Casbin -> HTTP
├── config.json                   # static config (viper overlay target)
├── internal/
│   ├── auth/manager.go           # OIDC RP: PKCE, exchange, refresh, revoke, JWKS verify, userinfo, role claim
│   ├── authz/enforcer.go         # Casbin enforcer + RequirePermission middleware (401/403)
│   ├── config/config.go          # config.json + env merge (viper)
│   └── api/server.go             # routes: /login /auth/callback /api/me /auth/refresh /logout + guarded sample routes
├── deploy/
│   ├── docker-compose.yml        # Zitadel v4.17.2 + Postgres 17, PAT-at-init bootstrap
│   ├── zitadel-config.yaml       # minimal Zitadel config
│   ├── model.conf                # Casbin RBAC model
│   ├── policy.csv                # role -> permission (module.resource.action) seeds
│   ├── .env                      # compose vars (dev defaults, NOT secrets)
│   └── bootstrap/                # git-ignored PATs written by init
├── scripts/
│   ├── provision.sh              # headless seeding via Management API (PAT)
│   └── e2e-demo.sh               # headless E2E acceptance (Session API as fake Login UI)
└── internal/..._test.go          # unit tests: RBAC matrix, role-claim shape, PKCE RFC vector, config merge
```

### Flow

1. `GET /login` → server generates `state` + PKCE `code_verifier` (kept in a
   10-min TTL map), 302 to Zitadel `/oauth/v2/authorize` with `S256` challenge.
2. User authenticates (browser: Zitadel Login; headless: Session API with the
   `login-client` PAT, same call the Login UI makes).
3. `GET /auth/callback?code&state` → verifier swapped for tokens at
   `/oauth/v2/token`; ID token verified (issuer/audience/expiry via JWKS);
   userinfo fetched for `loginName`/display name; Zitadel role claim flattened;
   server-side session in-memory + HttpOnly SameSite=Lax cookie.
4. Every guarded route: cookie → session → identity into context →
   Casbin `Enforce(subject, "module.resource.action")` → 200 / 401 / 403.
5. `POST /auth/refresh` rotates tokens via refresh grant (new expiry proven in E2E).
6. `POST /logout` deletes session + best-effort revokes refresh token at
   `/oauth/v2/revoke`.

### Casbin model (deploy/model.conf)

```ini
[request_definition]
r = sub, perm

[policy_definition]
p = role, perm

[policy_effect]
e = some(where (p.eft == allow))

[role_definition]
g = _, _

[matchers]
m = g(r.sub, p.role) && globMatch(r.perm, p.perm)
```

Policies (deploy/policy.csv):

```csv
p, admin, *
p, accountant, accounting.*
p, accountant, assets.asset.read
p, asset-manager, assets.*
p, viewer, *.read

g, <oidc-sub>, admin      # bound at login from role claims (in-memory, spike)
g, <oidc-sub>, viewer
```

- Permissions are `module.resource.action` strings, matching PRD F0.5.
- `globMatch` gives cheap wildcarding (`*` = admin, `*.read` = read-only).
- Subjects are OIDC token subjects; bindings added lazily per request from the
  token's role claim (`EnsureBinding`, idempotent). Production would persist or
  re-derive bindings per request — flagged for ARCA-30.
- RBAC matrix is unit-tested (`internal/authz/enforcer_test.go`), including the
  PRD case: viewer POST journal → denied.

## Dev setup (from zero)

```bash
cd spikes/arca27-oidc-casbin

# 1. Boot identity provider (first init writes admin PATs to deploy/bootstrap/)
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
# wait for: curl http://localhost:8888/debug/ready -> "ok"

# 2. Seed org/project/roles/app/users/grants; writes deploy/.env.local
bash scripts/provision.sh

# 3. Run the spike server (needs Go 1.26+)
set -a; source deploy/.env.local; set +a
go run ./cmd/server

# 4. Acceptance, headless, no browser:
bash scripts/e2e-demo.sh
# OR manually: open http://localhost:7777/ and sign in as
#   admin@acme.local / Password1!   (role: admin)
#   viewer@acme.local / Password1!  (role: viewer)
```

Clean slate: `docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v`
and delete `deploy/bootstrap/` + `deploy/.env.local`, then re-run 1–2.

## Configuration the platform slice will need (ARCA-29/30 contract)

### `.env` (secrets — never in config.json)

| Variable | Purpose | Example |
|---|---|---|
| `OIDC_ISSUER` | issuer + discovery base | `http://localhost:8888` |
| `OIDC_CLIENT_ID` | OIDC app client id (from provisioning) | `389092298769629187` |
| `OIDC_CLIENT_SECRET` | empty for PKCE web/native apps | *(empty)* |
| `OIDC_REDIRECT_URI` | callback URL registered in the app | `http://localhost:7777/auth/callback` |
| `APP_SECRET` | cookie signing/encryption key | `openssl rand -hex 32` |
| `ZITADEL_MASTERKEY` | Zitadel init masterkey (exactly 32 chars) | `MasterkeyNeedsToHave32Characters` |

Supporting (compose only): `ZITADEL_VERSION`, `ZITADEL_PORT`, `ZITADEL_ADMIN_*`,
`ZITADEL_ORG_NAME`, `ZITADEL_INSTANCE_NAME`.

### `config.json` keys (static, non-secret)

```json
{
  "server": { "port": 7777, "base_url": "http://localhost:7777" },
  "oidc": {
    "scopes": ["openid", "profile", "email", "offline_access"],
    "cookie_name": "arca27_session"
  },
  "authz": { "model_path": "deploy/model.conf", "policy_path": "deploy/policy.csv" }
}
```

Env overrides (viper): `APP_PORT`, `APP_BASE_URL`, `OIDC_*` (documented above).
Merging is proven by `internal/config/config_test.go`.

### Platform-slice notes for ARCA-30

- **offline_access scope is required** for refresh tokens; `projectRoleAssertion`
  on the project + `accessTokenRoleAssertion` on the app put roles into tokens.
- Role claim path: `urn:zitadel:iam:org:project:roles` =
  `{roleKey: {orgId: domain}}` (see `internal/auth/manager_test.go`). Parse from
  both ID and access tokens; treat missing claim as "no roles".
- Zitadel's ID token omits `loginName`/`given_name` — call userinfo once at
  callback (already implemented) instead of guessing claim shapes.
- Headless/E2E auth = Session API (`POST /v2/sessions` with checks) +
  `POST /v2/oidc/auth_requests/{id}` with `sessionToken` — authorize with the
  `login-client` PAT (bootstrap env vars in compose). User-PAT
  authorization on those endpoints fails with `AUTHZ-cdgFk` (membership check).
- The in-memory session store and role bindings are spike-only: platform slice
  should sign cookies (APP_SECRET) or persist sessions server-side.
- Consider a tiny "authz sync" job instead of lazy binding if permission
  revocation must be immediate.

## Verify (evidence)

```text
$ go test ./...
ok  internal/auth      (role claim shape, PKCE RFC-7636 vector, JWT pretty-print)
ok  internal/authz     (13-case RBAC matrix incl. viewer-403, idempotent binding)
ok  internal/config    (defaults, env override, incomplete-config rejection)

$ bash scripts/e2e-demo.sh
PASS: viewer login completed (code exchanged)
PASS: admin login completed (code exchanged)
viewer roles: ['viewer'] / admin roles: ['admin']
PASS: viewer GET  /api/assets          (200)
PASS: viewer POST /api/assets          (403)
PASS: viewer POST /api/accounting/journal (403)   <- PRD AC#3 demo
PASS: admin  POST /api/assets          (201)
PASS: admin  POST /api/accounting/journal (201)
PASS: unauthenticated GET /api/assets  (401)
PASS: token refresh (new expiry issued)
PASS: logout revokes + clears session  (200) → post-logout 401
RESULT: 13 passed, 0 failed
```
