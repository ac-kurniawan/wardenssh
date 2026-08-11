# Design Spec: Vault Session Persistence & OS Keyring Integration

**Date:** 2026-08-11
**Feature:** Automatic Vault Unlock via OS Keyring Session Tokens

## 1. Overview
WardenSSH integrates cross-platform OS keyring storage (`go-keyring` / Windows Credential Manager / macOS Keychain / Linux Secret Service) to persist vault refresh tokens. Upon app launch, WardenSSH attempts auto-login for configured vaults using stored keyring tokens. If valid, the vault unlocks seamlessly without prompting for master password. On manual unlock via `SetupModal`, successful login tokens are saved to the OS keyring.

## 2. Keyring Seam & API Integration

### 2.1 Storage Contract (`internal/keyring`)
- Service Name: `wardenssh`
- Key Name: `vw:<vaultName>` (e.g. `vw:personal`, `vw:work`)
- Value: Vault Refresh Token / Access Token session payload.

### 2.2 Setup Modal Auto-Login Workflow (`internal/tviewui/setup.go`)
- On `SetupModal` initialization:
  - Check `keyring.GetRefreshToken(vault.Name)`.
  - If token exists: attempt async refresh token login via `vaultclient.Client.RefreshToken(token)`.
  - On success: sync vault items, add `vaultadapter.Source` to `App`, and advance setup automatically.
  - On failure (expired/missing): prompt user for Master Password.
- On manual login success:
  - Call `keyring.SetRefreshToken(vault.Name, session.RefreshToken)`.

## 3. Testing Strategy
- Unit test keyring save/load/delete with mock/fake keyring.
- Unit test `SetupModal` auto-login with valid and expired refresh tokens.
