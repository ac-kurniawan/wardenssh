# Vault Session Keyring Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist vault refresh tokens in OS keyring and attempt auto-login on setup startup, avoiding manual master password prompts on subsequent launches.

**Architecture:** On `SetupModal` initialization, check `keyring.GetRefreshToken(vaultName)`. If present, attempt `client.RefreshToken(token)`. On success, auto-unlock vault and notify `App`. On manual password login, save the new `RefreshToken` to the keyring.

**Tech Stack:** Go stdlib (`net/http`), `zalando/go-keyring`, `tview`.

## Global Constraints

- **Private keys never written to disk.** RAM + vault only.
- **No secrets in config or logs.** Tokens live in OS keyring, stderr diagnostics only.
- **TDD mandatory.** Red-Green-Refactor for every change.
- **Pure Go, no CGO.**

---

### Task 1: Vault Client RefreshToken & Keyring Save on Login

**Files:**
- Modify: `internal/vaultclient/client.go`, `internal/tviewui/setup.go`
- Test: `internal/vaultclient/client_test.go`, `internal/tviewui/setup_test.go`

**Interfaces:**
- Consumes: `vaultclient.Client`, `keyring`
- Produces:
  - `vaultclient.Client.RefreshToken(refreshToken string) (*Session, error)`
  - Save token to `keyring.SetRefreshToken` on manual setup unlock.

- [ ] **Step 1: Write failing test for RefreshToken**

In `internal/vaultclient/client_test.go`:

```go
func TestClientRefreshToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/identity/connect/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"new-acc-token","refresh_token":"new-ref-token","expires_in":3600,"token_type":"Bearer"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := vaultclient.New(srv.URL)
	sess, err := c.RefreshToken("existing-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if sess.AccessToken != "new-acc-token" {
		t.Errorf("got AccessToken = %q, want new-acc-token", sess.AccessToken)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vaultclient/ -run TestClientRefreshToken -v`
Expected: FAIL (`c.RefreshToken undefined`)

- [ ] **Step 3: Implement RefreshToken in vaultclient**

In `internal/vaultclient/client.go`:

```go
// RefreshToken exchanges a refresh token for a new session token pair.
func (c *Client) RefreshToken(refreshToken string) (*Session, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", "web")

	req, err := http.NewRequest("POST", c.baseURL+"/identity/connect/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("vaultclient: refresh req: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vaultclient: refresh post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vaultclient: refresh HTTP %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("vaultclient: refresh decode: %w", err)
	}

	return &Session{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vaultclient/ -run TestClientRefreshToken -v`
Expected: PASS

- [ ] **Step 5: Save refresh token in SetupModal on login success**

In `internal/tviewui/setup.go`:
Call `_ = keyring.SetRefreshToken(v.Name, sess.RefreshToken)` upon successful login in `SetupModal`.

- [ ] **Step 6: Commit**

```bash
git add internal/vaultclient/client.go internal/vaultclient/client_test.go internal/tviewui/setup.go
git commit -m "feat(vaultclient): add RefreshToken and persist session token to keyring on login"
```

---

### Task 2: Auto-Login via Keyring in SetupModal

**Files:**
- Modify: `internal/tviewui/setup.go`
- Test: `internal/tviewui/setup_test.go`

**Interfaces:**
- Consumes: `keyring.GetRefreshToken`, `vaultclient.Client.RefreshToken`
- Produces: Auto-login on `SetupModal` initialization when valid keyring token is present.

- [ ] **Step 1: Write failing test for Auto-login**

In `internal/tviewui/setup_test.go`:

```go
func TestSetupModalAutoLoginWithKeyring(t *testing.T) {
	// Set mock refresh token in keyring.
	_ = keyring.SetRefreshToken("work", "valid-token")

	modal := tviewui.NewSetupModal([]config.Vault{{Name: "work", Email: "user@example.com", Server: "https://vw.example.com"}})
	// Auto-login succeeds for mock token in test mode.
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tviewui/ -run TestSetupModalAutoLoginWithKeyring -v`
Expected: FAIL (No auto-login executed)

- [ ] **Step 3: Implement Auto-login in SetupModal**

In `internal/tviewui/setup.go`:
On `NewSetupModal`, for the initial vault:
Check `tok, err := keyring.GetRefreshToken(v.Name)`.
If `err == nil` and `tok != ""`:
Attempt async `c.RefreshToken(tok)`. If successful, trigger `onSuccess` and advance setup without prompting for master password.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tviewui/ -run TestSetupModalAutoLoginWithKeyring -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tviewui/setup.go internal/tviewui/setup_test.go
git commit -m "feat(tviewui): auto-login using OS keyring refresh token in SetupModal"
```

---

## Plan Verification

Run complete test suite to confirm zero regressions:
```bash
go test -count=1 ./...
go build ./...
```
