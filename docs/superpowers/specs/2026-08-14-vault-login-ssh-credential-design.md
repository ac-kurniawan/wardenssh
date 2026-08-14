# Vault login (username/password) as SSH credential — Design

Date: 2026-08-14

## Goal

Allow a BitWarden/VaultWarden **Login item** (native `username` + `password`) to
be used as an SSH password credential, alongside the existing SSH-Key items.
A login item is a launchable host only when it carries the custom fields:

1. `host` — the SSH host (same opt-in convention as SSH-Key items), and
2. `type` with value `SSH` — mandatory filter; logins without it never appear.

## Decisions (confirmed with user)

- **Password delivery:** SSH_ASKPASS helper. WardenSSH re-execs itself with a
  hidden mode; `ssh` spawns it and reads the password from its stdout.
- **Helper secret channel:** env var. The password is placed in ssh's
  environment (`WARDENSSH_ASKPASS_PASS`); the askpass child inherits it. It
  lives in the short-lived ssh + helper processes only — never on disk, never
  in config, never logged. Matches the v0 "correctness not adversarial" threat
  model.
- Internal shape: **unified pipeline** — `vaultadapter.Items()` returns both
  SSH-Key and login hosts; connect branches on the item kind.

## Architecture / data flow

### 1. Vault parsing (`internal/vaultclient`)

`Cipher` gains a Login object (login ciphers are Type 1 on VaultWarden):

```go
Login *struct {
    Username string `json:"username"` // encrypted string
    Password string `json:"password"` // encrypted string
} `json:"login,omitempty"`
```

Username/password are BitWarden encrypted strings decrypted via the existing
`Session.DecryptField` (AES-CBC + HMAC, in RAM only).

### 2. Item model (`internal/vault`)

`vault.Item` gains:

```go
Kind         string // "sshkey" (default) | "login"
EncUsername  string // still-encrypted login username (lazy decrypt)
EncPassword  string // still-encrypted login password (lazy decrypt)
```

Same lazy-decrypt pattern as `EncPrivateKey`: fields decrypted eagerly at
list-build (name, host, user, port, proxyjump, type); credentials decrypted
only at connect time.

`vault.Source` gains:

```go
DecryptLogin(item Item) (username, password []byte, err error)
```

`FakeSource` implements it (returns `EncUsername`/`EncPassword` as plaintext
bytes for tests, mirroring `DecryptPrivateKey`).

### 3. Filtering (`internal/vaultadapter` + `internal/config`)

`config.CustomFields` gains a `Type` field (json `type`, default `"type"`),
added to `Default()` and `applyDefaults` so a partial config still resolves.

`vaultadapter.Source.Items()` returns, per cipher:

- **Type 5 (SSH-Key):** unchanged — requires `sshKey.privateKey` + a populated
  `host` field. `Kind` stays `"sshkey"`.
- **Type 1 (Login):** included only when all of:
  - `Login != nil`,
  - the `host` custom field is populated,
  - the `type` custom field value matches `"ssh"` case-insensitively.
  Produces `Kind:"login"`, `EncUsername`/`EncPassword` from the Login object,
  `User` from the decrypted `login.username` (fallback to the custom `user`
  field), `HostName`/`Port`/`ProxyJump` from the existing custom fields.

`readCustomFields` adds `Type` to the mapped values.

### 4. Host entry (`internal/hosts`)

`hosts.Entry` gains:

```go
AuthKind string // "" / "key" (default) | "password"
```

Set in `app.BuildHostList` / `app.VaultEntries` from `it.Kind`. List,
filter, scope cycling, and live-dot logic are untouched.

### 5. Askpass helper (`main.go`)

At the very top of `main`, before any config/vault init:

```go
if os.Getenv("WARDENSSH_ASKPASS") == "1" {
    fmt.Print(os.Getenv("WARDENSSH_ASKPASS_PASS"))
    os.Exit(0)
}
```

No vault access, no disk writes. The helper is spawned by ssh itself with the
executable path of the running binary.

### 6. Connect (`internal/connect`)

New functions:

- `SSHArgvPassword(entry)` — same as `SSHArgv` but **without
  `-o BatchMode=yes`** (batch mode forbids password prompts), user from
  `entry.User` with `root` fallback, keeps port/proxyjump/target.
- `EnvForAskpass(agentPipe, password string) []string` — sets
  `SSH_ASKPASS=<os.Executable()>`, `SSH_ASKPASS_REQUIRE=force`, and
  `WARDENSSH_ASKPASS_PASS=<password>` (plus `SSH_AUTH_SOCK` for parity).
- `PrepareLoginCreds(entry, vc) ([]byte, []byte, error)` — locate source +
  item via the existing `findVaultItem`, call `DecryptLogin`.

`SSH_ASKPASS_REQUIRE=force` (OpenSSH ≥ 8.4) is required because ssh runs
attached to a PTY (ConPTY on Windows), where askpass is normally ignored.

### 7. TUI connect flow (`internal/tviewui/app.go`)

`handleConnect` branches on `entry.AuthKind`:

- `key` → existing `connect.PrepareAgentKey` + `EnvForAgent`.
- `password` → `connect.PrepareLoginCreds` (failure → stderr + return), then
  `connect.SSHArgvPassword` + `connect.EnvForAskpass`.

The spawn path (`termPane.StartSSH`), live-dot marking, session bookkeeping,
disconnect, and quit-confirmation are shared and unchanged.

Host list badge: password hosts render a `[pw]` marker so they are visually
distinguishable from SSH-key hosts. Existing layout unchanged.

### 8. Security & lifecycle

- Password decrypted only at connect; in RAM + the short-lived ssh/askpass
  process env; never written to disk, config, or logs.
- Nothing retained in the agent for password hosts.
- Wrong password → ssh re-invokes askpass (up to 3 attempts) then exits with
  "Permission denied (password)". Accepted for v0; the failure is visible in
  the session terminal.

## Error handling

- Login item missing `host` or with non-SSH `type` → not listed (silent).
- `PrepareLoginCreds` failure (item not found / decrypt error) → message to
  stderr, connect aborts (same pattern as `PrepareAgentKey`).
- Askpass helper not honored by a platform's OpenSSH → connection fails with
  the ssh error visible in the terminal; gated by the verification spike.

## Testing (TDD, per AGENTS.md)

- `config`: `type` field default + partial-config resolution.
- `vaultclient`: `Cipher` parses the camelCase `login` object.
- `vaultadapter`: login items filtered by `type==ssh` + host; non-ssh type
  hidden; `Kind`, `EncUsername`, `EncPassword`, `User` populated correctly;
  SSH-Key items unaffected.
- `vault`: `FakeSource.DecryptLogin`.
- `hosts`/`app`: `AuthKind` propagates into `hosts.Entry`.
- `connect`: `SSHArgvPassword` has no BatchMode + user fallback;
  `EnvForAskpass` sets the three env vars; `PrepareLoginCreds` happy path +
  item-not-found.
- `main`: askpass mode prints the env password and exits without touching
  config/vault.
- **Gated verification spike:** real Windows OpenSSH 9.5p2 honoring
  `SSH_ASKPASS_REQUIRE=force` when spawned under a ConPTY-attached process —
  real connect to a local password-auth sshd (Docker or Win32-OpenSSH server).
  Mirrors Spike #1's gating. Fallback if it fails: in-process
  `golang.org/x/crypto/ssh` password client for password hosts.

## Out of scope

- 2FA for vault login (existing vault auth flow, unchanged).
- Keyboard-interactive-only servers beyond what askpass handles.
- Password storage in the OS keyring or config (never).
- Retry/correct-password UX inside the TUI (v1+).
