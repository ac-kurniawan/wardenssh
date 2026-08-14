# Vault Login (username/password) as SSH Credential — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow a BitWarden/VaultWarden **Login item** (native username + password) to launch an SSH password-authenticated session, alongside the existing SSH-Key hosts. A login item is launchable only when it has a populated `host` custom field **and** a `type` custom field equal to `SSH` (case-insensitive).

**Architecture:** Unified pipeline — `vaultadapter.Source.Items()` returns both SSH-Key items and filtered Login items; `vault.Item` gains a `Kind` (`sshkey`/`login`) with lazy `EncUsername`/`EncPassword`; `hosts.Entry` gains `AuthKind` (`key`/`password`). On connect, a `"password"` host has its credentials lazily decrypted and delivered to `ssh` via an `SSH_ASKPASS` helper (WardenSSH re-execs itself in a hidden askpass mode; the password travels in the short-lived ssh + helper environment, never to disk/config/logs).

**Tech Stack:** Go, stdlib `testing`, `golang.org/x/crypto/ssh`, tview/tvxterm/go-pty. No new dependencies.

## Global Constraints

- TDD is mandatory (AGENTS.md): write a failing test first, then the minimum code to pass, then refactor. No untested code ships.
- Commits must pass `go test ./...`. Commit small, one logical change per commit, on the working branch.
- Do not commit secrets: no real passwords, keys, tokens, hostnames, or vault URLs in fixtures — use generated/throwaway values.
- No secrets on disk or in logs: passwords are RAM + short-lived process env only. Never write them to `~/.ssh/`, config, temp files, or stdout/stderr logging.
- VaultWarden JSON keys are camelCase; do not introduce PascalCase tags.
- The SSH `type` filter matches the custom-field value case-insensitively equal to `"ssh"`.
- No CGO; keep the binary pure-Go and cross-compilable.
- Platform: Windows + Linux/macOS. The Windows `SSH_ASKPASS_REQUIRE=force` path is gated on a verification spike (Task 9).

---

### Task 1: Config — `type` custom-field name

**Files:**
- Modify: `internal/config/config.go` (CustomFields struct, `Default()`, `applyDefaults`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `config.CustomFields`, `config.Default()`, `applyDefaults`.
- Produces: `config.CustomFields.Type string` with JSON tag `type`, default value `"type"` in `Default()` and `applyDefaults`. Later tasks read this via `config.Default().CustomFields.Type`.

- [ ] **Step 1: Write the failing tests** (append to `internal/config/config_test.go`)

```go
// TestLoadAppliesTypeFieldDefault: a minimal config gets default type="type".
func TestLoadAppliesTypeFieldDefault(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CustomFields.Type != "type" {
		t.Errorf("CustomFields.Type default = %q, want type", cfg.CustomFields.Type)
	}
}

// TestLoadParsesTypeOverride: an explicit custom_fields.type is honored.
func TestLoadParsesTypeOverride(t *testing.T) {
	cfg, err := config.Load(strings.NewReader(`{"custom_fields":{"type":"kind"}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CustomFields.Type != "kind" {
		t.Errorf("CustomFields.Type = %q, want kind", cfg.CustomFields.Type)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestLoadAppliesTypeFieldDefault|TestLoadParsesTypeOverride' -v`
Expected: FAIL — `cfg.CustomFields.Type` does not exist yet (compile error).

- [ ] **Step 3: Implement**

In `internal/config/config.go`:
1. Add to the `CustomFields` struct:
```go
	// Type is the custom-field name whose value tags a Login item as an SSH
	// credential (value "SSH", case-insensitive). Logins without it are not
	// launchable.
	Type string `json:"type"`
```
2. In `Default()`:
```go
		CustomFields: CustomFields{
			Host:      "host",
			User:      "user",
			Port:      "port",
			ProxyJump: "proxyjump",
			Type:      "type",
		},
```
3. In `applyDefaults` (after the `ProxyJump` block):
```go
	if cfg.CustomFields.Type == "" {
		cfg.CustomFields.Type = d.Type
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add custom_fields.type for SSH login-item filter"
```

---

### Task 2: vaultclient — parse the `login` object

**Files:**
- Modify: `internal/vaultclient/client.go` (Cipher struct)
- Test: `internal/vaultclient/sync_test.go`

**Interfaces:**
- Consumes: existing `fakeCiphersServer` helper in `sync_test.go`, existing `Cipher` struct.
- Produces: `vaultclient.Login` struct (`Username`, `Password`) and `Cipher.Login *Login` with JSON tag `login`. Later tasks read `ci.Login.Username` / `ci.Login.Password` (encrypted strings).

- [ ] **Step 1: Write the failing test** (append to `internal/vaultclient/sync_test.go`)

```go
// TestSyncParsesLoginObject: a Type-1 login cipher's username+password are
// populated from the camelCase "login" object.
func TestSyncParsesLoginObject(t *testing.T) {
	ciphersJSON := `[
		{
			"id": "login-1",
			"name": "2.enc==",
			"type": 1,
			"login": {"username": "2.u==", "password": "2.p=="}
		}
	]`
	srv := fakeCiphersServer(t, ciphersJSON)
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "tok"}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(sr.Ciphers) != 1 {
		t.Fatalf("expected 1 cipher, got %d", len(sr.Ciphers))
	}
	ci := sr.Ciphers[0]
	if ci.Login == nil {
		t.Fatal("Login is nil")
	}
	if ci.Login.Username != "2.u==" {
		t.Errorf("Username = %q, want 2.u==", ci.Login.Username)
	}
	if ci.Login.Password != "2.p==" {
		t.Errorf("Password = %q, want 2.p==", ci.Login.Password)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vaultclient/ -run TestSyncParsesLoginObject -v`
Expected: FAIL — `ci.Login` is nil (field does not exist).

- [ ] **Step 3: Implement**

In `internal/vaultclient/client.go`, add a named type and extend `Cipher` (place near the `Cipher` struct definition):

```go
// Login is a Type-1 (login) item's native username/password, both encrypted
// strings decrypted via Session.DecryptField at use time.
type Login struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
```
Add to `Cipher`:
```go
	Login  *Login `json:"login,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vaultclient/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vaultclient/client.go internal/vaultclient/sync_test.go
git commit -m "feat(vaultclient): parse login cipher username/password"
```

---

### Task 3: vault — item kind + `Source.DecryptLogin` + fake

**Files:**
- Modify: `internal/vault/vault.go`
- Modify: `internal/connect/connect_test.go` (compile fix: local `fakeSource` must satisfy the widened `vault.Source` interface)
- Test: `internal/vault/vault_test.go`

**Interfaces:**
- Consumes: existing `vault.Item`, `vault.Source`, `vault.FakeSource`.
- Produces:
  - `vault.Item.Kind string` (`""`/`"sshkey"` default, `"login"`), `Item.EncUsername string`, `Item.EncPassword string`.
  - `vault.Source.DecryptLogin(item Item) (username, password []byte, err error)`.
  - `vault.FakeSource.DecryptLogin` returns `[]byte(item.EncUsername)`, `[]byte(item.EncPassword)`.
  - All `vault.Source` implementations (`vault.FakeSource`, `vaultadapter.Source`, and the test fake in `connect_test.go`) must implement the new method (compile gate).

- [ ] **Step 1: Write the failing test** (append to `internal/vault/vault_test.go`)

```go
// TestFakeSourceDecryptLogin: the fake returns the item's encrypted username
// and password as plaintext bytes (mirrors DecryptPrivateKey's behavior).
func TestFakeSourceDecryptLogin(t *testing.T) {
	src := vault.NewFakeSource("vw:personal", nil)
	user, pass, err := src.DecryptLogin(vault.Item{Kind: "login", EncUsername: "admin", EncPassword: "s3cret"})
	if err != nil {
		t.Fatalf("DecryptLogin: %v", err)
	}
	if string(user) != "admin" || string(pass) != "s3cret" {
		t.Errorf("user=%q pass=%q, want admin/s3cret", user, pass)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vault/ -run TestFakeSourceDecryptLogin -v`
Expected: FAIL — `DecryptLogin` not defined (compile error).

- [ ] **Step 3: Implement** (`internal/vault/vault.go`)

1. Add fields to `Item`:
```go
	// Kind is the item's credential kind: ""/"sshkey" (default) or "login".
	Kind string
	// EncUsername / EncPassword are the still-encrypted native login fields,
	// lazily decrypted at connect time via DecryptLogin.
	EncUsername string
	EncPassword string
```
2. Add the method to the `Source` interface:
```go
	// DecryptLogin decrypts the item's native login username + password into
	// raw bytes. Called at connect time, not at list-build time.
	DecryptLogin(item Item) (username, password []byte, err error)
```
3. Add the fake implementation:
```go
// DecryptLogin satisfies Source. For the fake, returns the EncUsername and
// EncPassword verbatim as plaintext bytes (tests pre-set them as plaintext).
func (s *FakeSource) DecryptLogin(item Item) ([]byte, []byte, error) {
	return []byte(item.EncUsername), []byte(item.EncPassword), nil
}
```
4. **Compile fix** — `internal/connect/connect_test.go`, `fakeSource`:
```go
func (f *fakeSource) DecryptLogin(it vault.Item) ([]byte, []byte, error) {
	return []byte(it.EncUsername), []byte(it.EncPassword), nil
}
```

- [ ] **Step 4: Run tests to verify compile + pass**

Run: `go test ./internal/vault/ ./internal/connect/`
Expected: PASS (compiles — the widened interface is satisfied everywhere).

- [ ] **Step 5: Commit**

```bash
git add internal/vault/vault.go internal/vault/vault_test.go internal/connect/connect_test.go
git commit -m "feat(vault): add login item kind + Source.DecryptLogin"
```

---

### Task 4: vaultadapter — login filtering + item build + `DecryptLogin`

**Files:**
- Modify: `internal/vaultadapter/adapter.go`
- Test: `internal/vaultadapter/adapter_test.go`

**Interfaces:**
- Consumes: `vaultclient.Login` (Task 2), `vault.Item.Kind/EncUsername/EncPassword` + `vault.Source.DecryptLogin` (Task 3), `config.CustomFields.Type` (Task 1), `vaultclient.Session.DecryptField`.
- Produces:
  - `vaultadapter.Source.Items()` now returns SSH-Key items AND Login items filtered by `host` populated + `type` value `== "ssh"` (case-insensitive). Login items carry `Kind:"login"`, `EncUsername`, `EncPassword`, and `User` from the decrypted `login.username` (fallback to the custom `user` field).
  - `vaultadapter.Source.DecryptLogin(item)` decrypts `item.EncUsername`/`item.EncPassword`.

- [ ] **Step 1: Write the failing tests** (append to `internal/vaultadapter/adapter_test.go`, package `vaultadapter_test`; reuse the existing `fakeSession` and `enc` helpers and `config.Default().CustomFields`)

```go
// TestSourceItemsIncludesSSHLoginItems: login items are launchable only when
// they have a populated host AND a type==SSH (case-insensitive) custom field.
func TestSourceItemsIncludesSSHLoginItems(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields

	mkLogin := func(id, name, u, p string, fields []vaultclient.CustomField) vaultclient.Cipher {
		return vaultclient.Cipher{
			ID:   id,
			Name: enc(t, sess, name),
			Type: 1,
			Login: &vaultclient.Login{
				Username: enc(t, sess, u),
				Password: enc(t, sess, p),
			},
			Fields: fields,
		}
	}

	ciphers := []vaultclient.Cipher{
		// login with type==SSH + host -> included, user from login.username
		mkLogin("1", "prod-db", "admin", "s3cret", []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.9"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "SSH"), Type: 0},
		}),
		// login with type != SSH -> excluded
		mkLogin("2", "web-ui", "u", "p", []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "web.internal"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "HTTPS"), Type: 0},
		}),
		// login with no type field -> excluded
		mkLogin("3", "ftp-box", "u", "p", []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "ftp.internal"), Type: 0},
		}),
		// login without host -> excluded even with type=ssh
		mkLogin("4", "no-host", "u", "p", []vaultclient.CustomField{
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "ssh"), Type: 0},
		}),
	}

	src := vaultadapter.NewSource("vw:personal", sess, ciphers, cf)
	items, err := src.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (only prod-db qualifies)", len(items))
	}
	it := items[0]
	if it.Kind != "login" {
		t.Errorf("Kind = %q, want login", it.Kind)
	}
	if it.Name != "prod-db" || it.HostName != "10.0.0.9" || it.User != "admin" {
		t.Errorf("item = %+v", it)
	}
	// credentials stay encrypted (lazy decrypt at connect).
	if it.EncUsername == "admin" {
		t.Error("EncUsername is plaintext — should be the encrypted form")
	}
}

// TestSourceItemsSSHLoginTypeCaseInsensitive: type value matches case-insensitively.
func TestSourceItemsSSHLoginTypeCaseInsensitive(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields
	ci := vaultclient.Cipher{
		ID:   "1",
		Name: enc(t, sess, "h"),
		Type: 1,
		Login: &vaultclient.Login{Username: enc(t, sess, "u"), Password: enc(t, sess, "p")},
		Fields: []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "1.2.3.4"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "ssh"), Type: 0},
		},
	}
	items, err := vaultadapter.NewSource("vw:personal", sess, []vaultclient.Cipher{ci}, cf).Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("lowercase 'ssh' type should match, got %d items", len(items))
	}
}

// TestSourceDecryptLogin: lazy decrypt returns the original username+password.
func TestSourceDecryptLogin(t *testing.T) {
	sess := fakeSession(t)
	cf := config.Default().CustomFields
	ci := vaultclient.Cipher{
		ID:   "1",
		Name: enc(t, sess, "prod-db"),
		Type: 1,
		Login: &vaultclient.Login{Username: enc(t, sess, "admin"), Password: enc(t, sess, "s3cret")},
		Fields: []vaultclient.CustomField{
			{Name: enc(t, sess, "host"), Value: enc(t, sess, "10.0.0.9"), Type: 0},
			{Name: enc(t, sess, "type"), Value: enc(t, sess, "SSH"), Type: 0},
		},
	}
	src := vaultadapter.NewSource("vw:personal", sess, []vaultclient.Cipher{ci}, cf)
	items, _ := src.Items()
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	user, pass, err := src.DecryptLogin(items[0])
	if err != nil {
		t.Fatalf("DecryptLogin: %v", err)
	}
	if string(user) != "admin" || string(pass) != "s3cret" {
		t.Errorf("user=%q pass=%q, want admin/s3cret", user, pass)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vaultadapter/ -run 'TestSourceItemsIncludesSSHLoginItems|TestSourceItemsSSHLoginTypeCaseInsensitive|TestSourceDecryptLogin' -v`
Expected: FAIL — login items are currently dropped (no branch handles `ci.Login`).

- [ ] **Step 3: Implement** (`internal/vaultadapter/adapter.go`)

1. Add `"strings"` to the imports.
2. Extend `customFieldValues` and `readCustomFields`:
```go
type customFieldValues struct {
	HostName  string
	User      string
	Port      string
	ProxyJump string
	Type      string
}
```
```go
	v.Type = decrypted[cf.Type]
```
3. Replace the body of `Items()` with:
```go
func (s *Source) Items() ([]vault.Item, error) {
	var out []vault.Item
	for _, ci := range s.ciphers {
		// Decrypt the item name (display label, Q30/A).
		nameBytes, err := s.session.DecryptField(ci.Name)
		if err != nil {
			continue // skip items we can't decrypt
		}

		// Read custom fields via configurable names (Q16/B).
		cf := readCustomFields(s.session, ci.Fields, s.fields)

		// Q32/B: only items with a populated 'host' custom field are launchable.
		if cf.HostName == "" {
			continue
		}

		switch {
		case ci.Login != nil && strings.EqualFold(cf.Type, "ssh"):
			// Login item tagged type=SSH -> password-credential host.
			// Username is decrypted for display (User); the credentials stay
			// encrypted for lazy decrypt at connect time (Q8/C pattern).
			uname, _ := s.session.DecryptField(ci.Login.Username)
			item := vault.Item{
				ID:          ci.ID,
				Name:        string(nameBytes),
				Kind:        "login",
				HostName:    cf.HostName,
				User:        string(uname),
				Port:        cf.Port,
				ProxyJump:   cf.ProxyJump,
				EncUsername: ci.Login.Username,
				EncPassword: ci.Login.Password,
			}
			if item.User == "" {
				item.User = cf.User
			}
			out = append(out, item)
		case ci.SshKey != nil && ci.SshKey.PrivateKey != "":
			// SSH-Key item (existing path).
			item := vault.Item{
				ID:            ci.ID,
				Name:          string(nameBytes),
				HostName:      cf.HostName,
				User:          cf.User,
				Port:          cf.Port,
				ProxyJump:     cf.ProxyJump,
				EncPrivateKey: ci.SshKey.PrivateKey,
			}
			if ci.SshKey.Passphrase != "" {
				item.EncPassphrase = ci.SshKey.Passphrase
			}
			out = append(out, item)
		}
	}
	return out, nil
}
```
4. Add `DecryptLogin`:
```go
// DecryptLogin satisfies vault.Source: lazily decrypts the item's native
// login username + password (Q8/C pattern). Called at connect time.
func (s *Source) DecryptLogin(item vault.Item) ([]byte, []byte, error) {
	username, err := s.session.DecryptField(item.EncUsername)
	if err != nil {
		return nil, nil, fmt.Errorf("vaultadapter: decrypt login username: %w", err)
	}
	password, err := s.session.DecryptField(item.EncPassword)
	if err != nil {
		return nil, nil, fmt.Errorf("vaultadapter: decrypt login password: %w", err)
	}
	return username, password, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vaultadapter/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vaultadapter/adapter.go internal/vaultadapter/adapter_test.go
git commit -m "feat(vaultadapter): surface SSH-tagged login items as password hosts"
```

---

### Task 5: hosts + app — `Entry.AuthKind` propagation

**Files:**
- Modify: `internal/hosts/list.go` (`Entry` struct)
- Modify: `internal/app/app.go` (`BuildHostList`, `VaultEntries`)
- Test: `internal/app/app_test.go`

**Interfaces:**
- Consumes: `vault.Item.Kind` (Task 3).
- Produces: `hosts.Entry.AuthKind string` — `"key"` for SSH-Key/file entries, `"password"` for login items. `app.BuildHostList` and `app.VaultEntries` set it from `it.Kind`. Later tasks read `entry.AuthKind` in connect/TUI.

- [ ] **Step 1: Write the failing test** (append to `internal/app/app_test.go`)

```go
// TestBuildHostListPropagatesAuthKind: vault login items become "password"
// hosts; SSH-Key items become "key" hosts.
func TestBuildHostListPropagatesAuthKind(t *testing.T) {
	vc := vault.NewFakeClient(
		vault.NewFakeSource("vw:personal", []vault.Item{
			{Name: "ci-box", HostName: "10.1.0.10", Kind: "sshkey"},
			{Name: "prod-db", HostName: "10.0.0.9", Kind: "login"},
		}),
	)
	l, err := app.BuildHostList(nil, vc)
	if err != nil {
		t.Fatalf("BuildHostList: %v", err)
	}
	got := map[string]string{}
	for _, e := range l.All() {
		got[e.Alias] = e.AuthKind
	}
	if got["ci-box"] != "key" {
		t.Errorf("ci-box AuthKind = %q, want key", got["ci-box"])
	}
	if got["prod-db"] != "password" {
		t.Errorf("prod-db AuthKind = %q, want password", got["prod-db"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestBuildHostListPropagatesAuthKind -v`
Expected: FAIL — `e.AuthKind` does not exist yet (compile error).

- [ ] **Step 3: Implement**

In `internal/hosts/list.go`, add to `Entry`:
```go
	AuthKind string // "key" (default) | "password" — how connect authenticates
```
In `internal/app/app.go`, add a small helper and set it on both entry-building loops:
```go
// authKind maps a vault item kind to a hosts.Entry auth kind.
func authKind(itemKind string) string {
	if itemKind == "login" {
		return "password"
	}
	return "key"
}
```
In `BuildHostList`, the vault loop's `hosts.Entry{...}` literal gains:
```go
				AuthKind:  authKind(it.Kind),
```
In `VaultEntries`, the same:
```go
				AuthKind:  authKind(it.Kind),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/app/ ./internal/hosts/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/hosts/list.go internal/app/app.go internal/app/app_test.go
git commit -m "feat(hosts): propagate login auth kind into host entries"
```

---

### Task 6: connect — askpass argv/env + login creds + `CommandFor`

**Files:**
- Modify: `internal/connect/connect.go`
- Test: `internal/connect/connect_test.go`

**Interfaces:**
- Consumes: `hosts.Entry.AuthKind` (Task 5), `vault.Source.DecryptLogin` + `vault.Item` (Task 3), existing `findVaultItem`, `SSHArgv`, `EnvForAgent`, `PrepareAgentKey`.
- Produces:
  - `connect.SSHArgvPassword(entry hosts.Entry) []string` — ssh argv WITHOUT `BatchMode=yes` (batch mode forbids password auth), keeps StrictHostKeyChecking/port/proxyjump/target, user from `entry.User` with `root` fallback.
  - `connect.EnvForAskpass(agentPipe, password string) []string` — `SSH_AUTH_SOCK`, `SSH_ASKPASS=<executable>`, `SSH_ASKPASS_REQUIRE=force`, `WARDENSSH_ASKPASS_PASS`.
  - `connect.PrepareLoginCreds(entry hosts.Entry, vc vault.Client) (username, password []byte, err error)`.
  - `connect.CommandFor(entry hosts.Entry, sessionID, agentPipe string, vc vault.Client, agent *sshagent.Keyring) (argv, env []string, err error)` — the single argv/env decision point the TUI calls (Task 8). Branches on `AuthKind`; returns error for a password host with no vault client.
  - Package var `askpassExecutable func() string` (overridable in tests).

- [ ] **Step 1: Write the failing tests** (append to `internal/connect/connect_test.go`)

```go
func TestSSHArgvPassword(t *testing.T) {
	origSSHBin := SSHBin
	defer func() { SSHBin = origSSHBin }()
	SSHBin = "ssh"

	tests := []struct {
		name     string
		entry    hosts.Entry
		expected []string
	}{
		{
			name:     "password host defaults to root",
			entry:    hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"},
			expected: []string{"ssh", "-o", "StrictHostKeyChecking=accept-new", "root@10.0.0.9"},
		},
		{
			name:     "password host with user and port",
			entry:    hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", User: "admin", Port: "2222", Source: "vw:personal", AuthKind: "password"},
			expected: []string{"ssh", "-o", "StrictHostKeyChecking=accept-new", "-p", "2222", "admin@10.0.0.9"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SSHArgvPassword(tc.entry)
			if len(got) != len(tc.expected) {
				t.Fatalf("expected argv len %v, got %v\ngot: %v", len(tc.expected), len(got), got)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("argv mismatch at index %d: expected %q, got %q", i, tc.expected[i], got[i])
				}
			}
		})
	}
}

func TestSSHArgvPasswordOmitsBatchMode(t *testing.T) {
	argv := SSHArgvPassword(hosts.Entry{Alias: "h", HostName: "h.internal", Source: "vw:personal", AuthKind: "password"})
	for _, a := range argv {
		if a == "BatchMode=yes" {
			t.Errorf("password argv must not set BatchMode (forbids password auth): %v", argv)
		}
	}
}

func TestEnvForAskpass(t *testing.T) {
	orig := askpassExecutable
	askpassExecutable = func() string { return `C:\wardenssh\wardenssh.exe` }
	defer func() { askpassExecutable = orig }()

	env := EnvForAskpass(`\\.\pipe\wardenssh-agent`, "hunter2")
	want := map[string]string{
		"SSH_AUTH_SOCK":          `\\.\pipe\wardenssh-agent`,
		"SSH_ASKPASS":            `C:\wardenssh\wardenssh.exe`,
		"SSH_ASKPASS_REQUIRE":    "force",
		"WARDENSSH_ASKPASS_PASS": "hunter2",
	}
	if len(env) != len(want) {
		t.Fatalf("got %d env entries, want %d: %v", len(env), len(want), env)
	}
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad env entry %q", kv)
		}
		if parts[1] != want[parts[0]] {
			t.Errorf("%s = %q, want %q", parts[0], parts[1], want[parts[0]])
		}
	}
}

func TestPrepareLoginCreds(t *testing.T) {
	fc := &fakeClient{sources: []vault.Source{
		&fakeSource{name: "vw:personal", items: []vault.Item{
			{Name: "prod-db", EncUsername: "admin", EncPassword: "s3cret"},
		}},
	}}
	user, pass, err := PrepareLoginCreds(hosts.Entry{Alias: "prod-db", Source: "vw:personal"}, fc)
	if err != nil {
		t.Fatalf("PrepareLoginCreds: %v", err)
	}
	if string(user) != "admin" || string(pass) != "s3cret" {
		t.Errorf("user=%q pass=%q, want admin/s3cret", user, pass)
	}
}

func TestCommandForPasswordHost(t *testing.T) {
	fc := &fakeClient{sources: []vault.Source{
		&fakeSource{name: "vw:personal", items: []vault.Item{
			{Name: "prod-db", Kind: "login", EncUsername: "admin", EncPassword: "s3cret"},
		}},
	}}
	entry := hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"}
	argv, env, err := CommandFor(entry, "sess-1", `\\.\pipe\wardenssh-agent`, fc, nil)
	if err != nil {
		t.Fatalf("CommandFor: %v", err)
	}
	for _, a := range argv {
		if a == "BatchMode=yes" {
			t.Errorf("password argv must not set BatchMode: %v", argv)
		}
	}
	havePass := false
	for _, kv := range env {
		if kv == "WARDENSSH_ASKPASS_PASS=s3cret" {
			havePass = true
		}
	}
	if !havePass {
		t.Errorf("env missing WARDENSSH_ASKPASS_PASS=s3cret: %v", env)
	}
}

func TestCommandForPasswordHostWithoutVaultClient(t *testing.T) {
	entry := hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"}
	if _, _, err := CommandFor(entry, "sess-1", "pipe", nil, nil); err == nil {
		t.Fatal("expected error for password host with nil vault client")
	}
}

func TestCommandForKeyHostMatchesSSHArgv(t *testing.T) {
	entry := hosts.Entry{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"}
	argv, env, err := CommandFor(entry, "sess-1", "/tmp/agent.sock", nil, nil)
	if err != nil {
		t.Fatalf("CommandFor: %v", err)
	}
	want := SSHArgv(entry, "/tmp/agent.sock")
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range argv {
		if argv[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, argv[i], want[i])
		}
	}
	if len(env) != 1 || env[0] != "SSH_AUTH_SOCK=/tmp/agent.sock" {
		t.Errorf("env = %v, want SSH_AUTH_SOCK only", env)
	}
}
```

Note: `TestEnvForAskpass`, `TestCommandForPasswordHost`, and `TestPrepareLoginCreds` need `"strings"` imported in `internal/connect/connect_test.go` (currently imported are `errors` and `testing`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/connect/ -run 'TestSSHArgvPassword|TestEnvForAskpass|TestPrepareLoginCreds|TestCommandFor' -v`
Expected: FAIL — functions not defined (compile error).

- [ ] **Step 3: Implement** (`internal/connect/connect.go`)

1. Add a package var near `SSHBin`/`AgentPipePath`:
```go
// askpassExecutable returns the path ssh uses to spawn the SSH_ASKPASS helper
// (the warden binary itself in askpass mode). Overridable for tests.
var askpassExecutable = func() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "wardenssh"
}
```
2. Add the new functions:
```go
// SSHArgvPassword builds the ssh argv for a password-credential host. Unlike
// SSHArgv it omits BatchMode (which forbids password prompts); the password
// reaches ssh via SSH_ASKPASS (see EnvForAskpass).
func SSHArgvPassword(entry hosts.Entry) []string {
	var args []string
	args = append(args, SSHBin)
	args = append(args, "-o", "StrictHostKeyChecking=accept-new")

	if entry.Port != "" {
		args = append(args, "-p", entry.Port)
	}
	if entry.ProxyJump != "" {
		args = append(args, "-J", entry.ProxyJump)
	}

	user := entry.User
	if user == "" {
		user = "root"
	}
	args = append(args, user+"@"+entry.HostName)
	return args
}

// EnvForAskpass returns the environment variables ssh needs to prompt for the
// vault password via an SSH_ASKPASS helper. SSH_ASKPASS_REQUIRE=force is
// required because ssh runs attached to a PTY where askpass is otherwise
// ignored (OpenSSH >= 8.4).
func EnvForAskpass(agentPipe, password string) []string {
	return []string{
		"SSH_AUTH_SOCK=" + agentPipe,
		"SSH_ASKPASS=" + askpassExecutable(),
		"SSH_ASKPASS_REQUIRE=force",
		"WARDENSSH_ASKPASS_PASS=" + password,
	}
}

// PrepareLoginCreds lazily decrypts the vault login credentials for a
// password-credential host entry (RAM only).
func PrepareLoginCreds(entry hosts.Entry, vc vault.Client) (username, password []byte, err error) {
	item, src, err := findVaultItem(vc, entry)
	if err != nil {
		return nil, nil, fmt.Errorf("find vault item: %w", err)
	}
	return src.DecryptLogin(item)
}

// CommandFor builds the ssh argv + env for a host entry, branching on its
// auth kind: password hosts get their login credentials lazily decrypted and
// the askpass env; vault key hosts get their key loaded into the agent;
// file hosts get the plain argv/env.
func CommandFor(entry hosts.Entry, sessionID, agentPipe string, vc vault.Client, agent *sshagent.Keyring) (argv, env []string, err error) {
	switch entry.AuthKind {
	case "password":
		if vc == nil {
			return nil, nil, fmt.Errorf("connect: password host %q but no vault client", entry.Alias)
		}
		_, password, err := PrepareLoginCreds(entry, vc)
		if err != nil {
			return nil, nil, err
		}
		return SSHArgvPassword(entry), EnvForAskpass(agentPipe, string(password)), nil
	default:
		if entry.Source != "file" && vc != nil && agent != nil {
			if err := PrepareAgentKey(entry, sessionID, vc, agent); err != nil {
				return nil, nil, err
			}
		}
		return SSHArgv(entry, agentPipe), EnvForAgent(agentPipe), nil
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/connect/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/connect/connect.go internal/connect/connect_test.go
git commit -m "feat(connect): askpass argv/env + CommandFor for password hosts"
```

---

### Task 7: main — askpass helper mode

**Files:**
- Modify: `main.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: nothing (runs before config/vault init).
- Produces: `main.runAskpass() bool` — if `WARDENSSH_ASKPASS == "1"`, prints `WARDENSSH_ASKPASS_PASS` to `askpassOutput` and returns true. `main.main()` calls it first and returns when true. Package var `askpassOutput io.Writer = os.Stdout` (overridable for tests). This is the helper ssh spawns via `SSH_ASKPASS` (Task 6).

- [ ] **Step 1: Write the failing tests** (append to `main_test.go`)

```go
// TestAskpassModePrintsPassword: in askpass mode the process prints the vault
// password to stdout and signals it handled the request.
func TestAskpassModePrintsPassword(t *testing.T) {
	t.Setenv("WARDENSSH_ASKPASS", "1")
	t.Setenv("WARDENSSH_ASKPASS_PASS", "hunter2")
	old := askpassOutput
	defer func() { askpassOutput = old }()
	var out bytes.Buffer
	askpassOutput = &out

	if !runAskpass() {
		t.Fatal("runAskpass returned false in askpass mode")
	}
	if out.String() != "hunter2" {
		t.Errorf("output = %q, want hunter2", out.String())
	}
}

// TestAskpassModeInactiveByDefault: without WARDENSSH_ASKPASS=1 the helper
// mode does not trigger.
func TestAskpassModeInactiveByDefault(t *testing.T) {
	t.Setenv("WARDENSSH_ASKPASS", "")
	if runAskpass() {
		t.Error("runAskpass returned true without WARDENSSH_ASKPASS=1")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run 'TestAskpassMode' -v`
Expected: FAIL — `runAskpass` / `askpassOutput` not defined (compile error).

- [ ] **Step 3: Implement** (`main.go`)

1. Add a package var next to `versionOutput`:
```go
// askpassOutput is the io.Writer for the SSH_ASKPASS helper mode (overridable
// for tests). ssh spawns this binary and reads the password from stdout.
var askpassOutput io.Writer = os.Stdout
```
2. Add `runAskpass`:
```go
// runAskpass detects the SSH_ASKPASS helper mode and prints the vault password
// to stdout, exiting without touching config/vault/agent. Returns true when it
// handled the request.
func runAskpass() bool {
	if os.Getenv("WARDENSSH_ASKPASS") != "1" {
		return false
	}
	fmt.Fprint(askpassOutput, os.Getenv("WARDENSSH_ASKPASS_PASS"))
	return true
}
```
3. Call it at the very top of `main()`:
```go
func main() {
	if runAskpass() {
		return
	}
	showVersion, noKeyring := parseFlags()
	...
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test .`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat(main): SSH_ASKPASS helper mode for vault passwords"
```

---

### Task 8: tviewui — connect via `CommandFor` + `[pw]` badge

**Files:**
- Modify: `internal/tviewui/app.go` (`handleConnect`)
- Modify: `internal/tviewui/hostlist.go` (`formatHostLine`)
- Test: `internal/tviewui/app_test.go`, `internal/tviewui/hostlist_test.go`

**Interfaces:**
- Consumes: `connect.CommandFor` (Task 6), `hosts.Entry.AuthKind` (Task 5).
- Produces: `handleConnect` builds argv/env via `CommandFor` (no inline key/askpass logic), and password hosts render a `[pw]` marker in their source badge.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tviewui/app_test.go` (package `tviewui_test`; `isLive` is defined in `disconnect_test.go`, same package):

```go
// TestAppConnectPasswordHostWithoutVaultClientDoesNotSpawn: a password host
// with no vault client must abort before spawning a session or marking live.
func TestAppConnectPasswordHostWithoutVaultClientDoesNotSpawn(t *testing.T) {
	hl := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"},
	})
	app := tviewui.New(hl, tviewui.Deps{}, nil)
	app.HandleConnectForTest(hosts.Entry{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"})
	if app.TerminalPane().IsRunning() {
		t.Error("expected no session spawned without a vault client")
	}
	if isLive(hl, "prod-db", "vw:personal") {
		t.Error("expected host not marked live when connect aborted")
	}
}
```

Append to `internal/tviewui/hostlist_test.go` (package `tviewui_test`):

```go
// TestHostListPanePasswordBadge: a password-credential host renders a "pw"
// marker in its source badge.
func TestHostListPanePasswordBadge(t *testing.T) {
	hl := hosts.NewList([]hosts.Entry{
		{Alias: "prod-db", HostName: "10.0.0.9", Source: "vw:personal", AuthKind: "password"},
		{Alias: "ci-box", HostName: "10.1.0.10", Source: "vw:personal"},
	})
	pane := tviewui.NewHostListPane(hl)
	pane.Refresh()
	text := pane.SelectedRenderText()
	if !strings.Contains(text, "pw") {
		t.Errorf("password host badge missing 'pw' in %q", text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tviewui/ -run 'TestAppConnectPasswordHostWithoutVaultClientDoesNotSpawn|TestHostListPanePasswordBadge' -v`
Expected: FAIL — without `CommandFor` the password path falls through to `PrepareAgentKey`/`SSHArgv` (the no-vault test panics or spawns), and the badge lacks `pw`.

- [ ] **Step 3: Implement**

In `internal/tviewui/app.go`, replace the middle of `handleConnect` (the `PrepareAgentKey` + `SSHArgv`/`EnvForAgent` block) with:

```go
	sessionID := key

	agentPipe := a.deps.AgentPipe
	if agentPipe == "" {
		agentPipe = connect.AgentPipePath()
	}

	argv, env, err := connect.CommandFor(entry, sessionID, agentPipe, a.deps.VaultCli, a.deps.Agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wardenssh: prepare connect: %v\n", err)
		return
	}
```

(Keep the existing `MarkLive` → `StartSSH` → `MarkDead`/`ReleaseSession` flow below unchanged.)

In `internal/tviewui/hostlist.go`, update `formatHostLine`:

```go
func formatHostLine(e hosts.Entry) string {
	liveDot := "  "
	if e.Live {
		liveDot = "[green]●[-] "
	}

	badge := fmt.Sprintf("[gray:black]%s[-]", e.Source)
	if e.AuthKind == "password" {
		badge = fmt.Sprintf("[gray:black]%s [yellow]pw[-][-]", e.Source)
	}

	hostInfo := e.Alias
	if e.HostName != "" && e.HostName != e.Alias {
		hostInfo = fmt.Sprintf("%-25s (%s)", e.Alias, e.HostName)
	}

	return fmt.Sprintf("%s%s %s", liveDot, padRight(hostInfo, 30), badge)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tviewui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/app.go internal/tviewui/hostlist.go internal/tviewui/app_test.go internal/tviewui/hostlist_test.go
git commit -m "feat(tviewui): connect password hosts via CommandFor + pw badge"
```

---

### Task 9: gated spike — Windows ssh.exe honors SSH_ASKPASS under ConPTY

**Files:**
- Create: `spike/askpass/askpass_test.go`

**Interfaces:**
- Consumes: `main.runAskpass` behavior (Task 7) via the built binary, `connect.EnvForAskpass` (Task 6).
- Produces: a gated, differential verification that the real platform `ssh` authenticates with a password obtained from our askpass helper, and the decision gate: **if this fails on Windows, the password-credential feature is blocked and must pivot to an in-process `golang.org/x/crypto/ssh` password client** (documented in the spec, `.local/plan.md` spike log).

**Context:** This mirrors Spike #1 (`spike/realssh`): a differential proof on the riskiest platform primitive. The risk is that `ssh.exe` (9.5p2) running under a ConPTY-attached process ignores `SSH_ASKPASS` even with `SSH_ASKPASS_REQUIRE=force`.

- [ ] **Step 1: Provision a password-auth sshd target**

Pick one:
- **Docker:** `docker run -d -p 2222:22 -e PASSWORD_AUTH=yes --name askpass-sshd panubo/sshd` (or any image with password auth enabled; use a throwaway account `test:testpass`).
- **Windows OpenSSH Server** (admin): `Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0`, enable `PasswordAuthentication yes` in `C:\ProgramData\ssh\sshd_config`, set a throwaway Windows user's password, start the service.

Verify with a manual interactive connect that `ssh -p 2222 test@localhost` prompts and succeeds with the test password.

- [ ] **Step 2: Write the gated spike test** (`spike/askpass/askpass_test.go`)

```go
// Package askpass is Spike #3 (see .local/plan.md): it verifies the real
// platform OpenSSH client honors SSH_ASKPASS_REQUIRE=force when spawned as a
// child process (the TUI spawns ssh via go-pty / ConPTY). The proof is
// differential: ssh + askpass helper with the CORRECT password succeeds;
// with the WRONG password it fails with Permission denied.
//
// GATED: skipped unless WARDENSSH_ASKPASS_SPIKE=1 (needs a provisioned
// password-auth sshd target; never run in normal testing/CI).
//
// SECURITY: the test password is a throwaway value for the throwaway sshd
// account only; it is never a real vault credential.
package askpass

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
)

const (
	testHost = "localhost"
	testPort = "2222"
	testUser = "test"
	goodPass = "testpass"
	badPass  = "wrongpass"
)

// askpassSh returns a tiny askpass helper (the same contract WardenSSH's
// runAskpass implements: print the password from WARDENSSH_ASKPASS_PASS).
func askpassSh(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sh := dir + "/askpass.sh"
	script := "#!/bin/sh\nprintf '%s' \"$WARDENSSH_ASKPASS_PASS\"\n"
	if err := os.WriteFile(sh, []byte(script), 0o700); err != nil {
		t.Fatalf("write askpass helper: %v", err)
	}
	return sh
}

func runSSH(t *testing.T, pass string) (string, string, error) {
	t.Helper()
	cmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile="+os.DevNull,
		"-o", "ConnectTimeout=10",
		"-p", testPort,
		testUser+"@"+testHost,
		"echo ASKPASS-OK",
	)
	cmd.Env = append(os.Environ(),
		"SSH_ASKPASS="+askpassSh(t),
		"SSH_ASKPASS_REQUIRE=force",
		"WARDENSSH_ASKPASS_PASS="+pass,
	)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), err
}

func TestAskpassCorrectPasswordAuthenticates(t *testing.T) {
	if os.Getenv("WARDENSSH_ASKPASS_SPIKE") != "1" {
		t.Skip("askpass spike skipped (set WARDENSSH_ASKPASS_SPIKE=1 to run)")
	}
	out, errOut, err := runSSH(t, goodPass)
	if err != nil {
		t.Fatalf("ssh with correct askpass password failed: %v\nstderr: %s", err, errOut)
	}
	if !bytes.Contains([]byte(out), []byte("ASKPASS-OK")) {
		t.Fatalf("no marker in output: %q", out)
	}
}

func TestAskpassWrongPasswordFails(t *testing.T) {
	if os.Getenv("WARDENSSH_ASKPASS_SPIKE") != "1" {
		t.Skip("askpass spike skipped (set WARDENSSH_ASKPASS_SPIKE=1 to run)")
	}
	_, errOut, err := runSSH(t, badPass)
	if err == nil {
		t.Fatal("ssh with WRONG askpass password unexpectedly succeeded")
	}
	if !bytes.Contains(errOut, []byte("Permission denied")) {
		t.Fatalf("expected Permission denied, got: %q", errOut)
	}
}
```

- [ ] **Step 3: Build the binary + run the spike**

Run (Windows PowerShell; set the env vars pointing at the provisioned target):
```powershell
go build -o C:\Users\ackur\AppData\Local\Temp\opencode\wardenssh.exe .
$env:WARDENSSH_ASKPASS_SPIKE = "1"
go test ./spike/askpass/ -v
```
Expected: PASS — both tests run against the real sshd; correct password authenticates, wrong password gets Permission denied.

- [ ] **Step 4: Commit the spike**

```bash
git add spike/askpass/askpass_test.go
git commit -m "test(spike): gated askpass verification against real sshd"
```

- [ ] **Step 5: Decision gate — update `.local/plan.md` spike log**

- If **PASS:** log the result; the password-credential feature is unblocked. Note any platform quirks observed (e.g. keyboard-interactive servers routing prompts through askpass, or requiring `ssh.exe` full path for `SSH_ASKPASS`).
- If **FAIL:** log it as a **blocking** spike (mirrors Spike #1). The password-credential feature must not ship until either (a) the askpass path is fixed (e.g. spawn ssh detached from console so askpass triggers without `force`, or use the `ssh.exe` full path), or (b) the design pivots to an in-process `golang.org/x/crypto/ssh` password client for password hosts (new spec/plan). Do not amend the failed commit — fix forward.

---

## Self-Review Notes

- **Spec coverage:** Task 1→`type` config field; Task 2→`login` parsing; Task 3→`Item.Kind` + `DecryptLogin`; Task 4→login filtering + item build; Task 5→`AuthKind` propagation; Task 6→askpass argv/env + creds + `CommandFor`; Task 7→askpass helper mode; Task 8→TUI connect branch + badge; Task 9→Windows verification spike. All spec sections (parsing, filtering, item model, host entry, helper, connect, TUI, security/lifecycle, testing) are covered.
- **Placeholder scan:** every step has real code; no TBD/TODO.
- **Type consistency:** `vault.Item.Kind`, `hosts.Entry.AuthKind`, `connect.CommandFor(entry, sessionID, agentPipe, vc, agent)` signature are defined once (Tasks 3/5/6) and consumed with matching names in Tasks 4/6/8. `wardensshExecutable`→`askpassExecutable`, `askpassOutput` naming is consistent between Task 6 and Task 7.
- **Deviation from spec:** Task 6 adds `connect.CommandFor` as the single argv/env decision point (spec left the branch inline in tviewui). This makes the connect branch unit-testable without spawning ssh; Task 8's `handleConnect` is thin wiring.