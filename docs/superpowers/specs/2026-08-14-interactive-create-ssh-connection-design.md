# Design Spec: Interactive Create New SSH Connection

## 1. Overview & Goals

This feature adds an interactive creation flow to WardenSSH, enabling users to create new SSH connections directly inside the TUI and save them either locally (`~/.ssh/config`) or to a BitWarden/VaultWarden vault.

### Core Goals
- **Interactive UI Modal**: Press `n` or `a` on the host list screen to launch a simple `tview.Form`-based creation dialog.
- **Dual Destination Support**:
  - `~/.ssh/config`: Append entry to `~/.ssh/config` and automatically generate SSH keys in `~/.ssh/`.
  - Vault: Create a new Cipher item in BitWarden/VaultWarden via Vault client API.
- **Credential Types**: Support **Password** and **Key** credentials.
- **Mandatory Vault Custom Fields**: Every Vault SSH entry created by WardenSSH must include custom fields `host` (target IP/hostname) and `type=SSH`.

---

## 2. Vault Item Schemas & Requirements

### 2.1 Password Credential (Vault)
- **Cipher Type**: `1` (Login Item)
- **Native Fields**: `username`, `password`
- **Custom Fields**:
  - `host` *(mandatory, custom field)*: IP or hostname
  - `type` *(mandatory, custom field)*: `SSH`
  - `port` *(optional)*: Port number string (e.g. `"22"`)
  - `proxyjump` *(optional)*: Bastion host string

### 2.2 Key Credential (Vault)
- **Cipher Type**: `5` (SSH-Key Item)
- **Key Generation**: Generated in RAM (`ed25519` by default, or `rsa` 4096). **Private key is never written to disk.**
- **Native Fields**:
  - `privateKey`: Decrypted key PEM string stored encrypted at rest in Vault
  - `publicKey`: Public key string
- **Custom Fields**:
  - `host` *(mandatory, custom field)*: IP or hostname
  - `type` *(mandatory, custom field)*: `SSH`
  - `user` *(optional)*: SSH username
  - `port` *(optional)*: Port number string
  - `proxyjump` *(optional)*: Bastion host string

---

## 3. Local `~/.ssh/config` & Key Generation

### 3.1 Password Credential (Local)
Appends a Host entry to `~/.ssh/config`:
```sshconfig
Host <alias>
    HostName <hostname>
    User <user>
    Port <port>
    ProxyJump <proxyjump>
```

### 3.2 Key Credential (Local)
1. Automatically generates an SSH keypair:
   - Target path: `~/.ssh/id_<algo>_<alias>` (e.g., `~/.ssh/id_ed25519_myserver`)
   - Permissions: Private key `0600`, Public key `0644`.
2. Appends Host entry to `~/.ssh/config`:
```sshconfig
Host <alias>
    HostName <hostname>
    User <user>
    Port <port>
    ProxyJump <proxyjump>
    IdentityFile ~/.ssh/id_<algo>_<alias>
```

---

## 4. UI Design & Interactive Form (`tviewui`)

### 4.1 Trigger & Shortcuts
- Key bindings on `hostlist`: `n` or `a` opens the `CreateConnectionModal`.
- Footer shortcut update: Displays `[n] New` in the list footer actions.

### 4.2 Form Layout & Validation
Modal Form fields:
1. **Alias / Name** *(Text Input, required)*
2. **Storage Destination** *(DropDown)*: `~/.ssh/config` or Vault names (e.g. `vw:personal`)
3. **Hostname / IP** *(Text Input, required)*
4. **User** *(Text Input, optional)*
5. **Port** *(Text Input, optional, defaults to 22)*
6. **ProxyJump** *(Text Input, optional)*
7. **Credential Type** *(DropDown)*: `Key (default)` or `Password`
8. **Conditional Fields**:
   - If Credential == `Password`:
     - **Password** *(PasswordField, required)*
   - If Credential == `Key`:
     - **Key Algorithm** *(DropDown)*: `Ed25519 (default)` or `RSA 4096`

### 4.3 Form Buttons & Handlers
- **[ Save ]**: Validates required fields (`Alias`, `HostName`, and credential data).
  - Performs background creation operation.
  - On success: Closes modal, merges new entry into `hosts.List`, and updates table UI.
  - On failure: Displays inline error message on modal header/title.
- **[ Cancel ] / Esc**: Closes modal without saving.

---

## 5. Architectural Components & Changes

1. `internal/sshconfig/writer.go`:
   - Functions `AppendHostEntry(path string, entry HostConfig) error`
   - Function `GenerateSSHKeyPair(algo string, path string) error`
2. `internal/vaultclient/create.go`:
   - Function `CreateCipher(sess *Session, item Cipher) (*Cipher, error)` for posting new encrypted items to Vault.
3. `internal/tviewui/create_modal.go`:
   - Implements `CreateConnectionModal` component for `tview`.
4. `internal/tviewui/app.go` & `hostlist.go`:
   - Integrates creation modal into application state and key handlers.

---

## 6. Verification & Testing Strategy

- **Unit Tests**:
  - `sshconfig.AppendHostEntry`: Verify parsing and appending to temporary `config` file.
  - `sshconfig.GenerateSSHKeyPair`: Verify file creation with permissions `0600` / `0644` and key validity via `ssh.ParseRawPrivateKey`.
  - `vaultclient.CreateCipher`: Mock HTTP server test for creating Login (Type 1) and SSH-Key (Type 5) items with custom fields (`host`, `type=SSH`).
  - `tviewui.CreateConnectionModal`: Test form validation, field toggling, submit and cancel callbacks.
- **End-to-End Test**:
  - Create host entry, verify appearance in `hosts.List`, test launching connection.
