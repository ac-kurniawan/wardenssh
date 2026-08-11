# WardenSSH

> **Cross-Platform SSH Management TUI with native Bitwarden & Vaultwarden Integration**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](#-cross-platform-support)
[![Security](https://img.shields.io/badge/Security-Zero--Disk--Footprint-success)](#-security-model)

**WardenSSH** is a modern, cross-platform Terminal User Interface (TUI) application designed to manage SSH host connections and private keys seamlessly. Built using the [Charm](https://charm.sh/) TUI stack ([Bubble Tea](https://github.com/charmbracelet/bubbletea), [Bubbles](https://github.com/charmbracelet/bubbles), and [Lip Gloss](https://github.com/charmbracelet/lipgloss)).

The core security guarantee of WardenSSH is **Zero-Disk-Footprint**: private keys stored in Bitwarden/Vaultwarden are **never written to disk**. Keys are decrypted strictly in RAM at rest and served directly to `ssh` via an **in-process SSH agent**.

---

## ✨ Features

- 🔐 **Native Bitwarden / Vaultwarden Integration**: Direct in-process integration with Bitwarden Password Manager API (supports custom fields, multi-vault, TOTP, and Email 2FA).
- 🔒 **Zero-Disk-Footprint Security**: Private keys remain encrypted at rest in your vault and exist only in RAM during execution. Served dynamically to `ssh` via an in-process agent.
- ⚡ **Multi-Source Host Aggregation**: Aggregate SSH hosts from multiple Bitwarden vaults alongside your local `~/.ssh/config`.
- 🔍 **Instant Fuzzy Search & Scoping**: Lightning-fast filtering by host name, alias, or source scope (`Tab` cycle between all sources, individual vaults, or `~/.ssh/config`).
- 🔄 **Parallel Session Management**: Launch and switch between multiple active SSH sessions with background PTY output buffering (`yield-and-switch`).
- 🔑 **Secure OS Keyring Storage**: Vault refresh tokens are stored securely in your operating system's native keyring (Windows Credential Manager, macOS Keychain, Linux Secret Service).
- 🌐 **Cross-Platform**: Full support for Linux, macOS, and Windows (utilizing Windows ConPTY and Named Pipes).
- 🔌 **Graceful Offline Mode**: Unreachable vaults degrade gracefully without breaking access to local `~/.ssh/config` hosts.

---

## 🏗️ Architecture & How It Works

WardenSSH acts as the **TUI Host Launcher**, **Vault Client**, and **In-Process SSH Agent** simultaneously. No external `bw` CLI binary or OS ssh-agent service is required.

```
                  ┌─────────────────────────────────────┐
                  │   Bitwarden / Vaultwarden Server    │
                  └──────────────────┬──────────────────┘
                                     │ Encrypted Sync (HTTPS)
                                     ▼
┌────────────────────────────────────────────────────────────────────────┐
│ WardenSSH (Single Binary Process)                                      │
│                                                                        │
│  ┌────────────────────┐   Decrypted    ┌────────────────────────────┐  │
│  │ Native Vault Client│ ─────────────> │ RAM Key Cache              │  │
│  └────────────────────┘   (In-RAM)     └─────────────┬──────────────┘  │
│                                                      │                 │
│  ┌────────────────────┐                ┌─────────────▼──────────────┐  │
│  │ Charm TUI Launcher │                │ In-Process SSH Agent       │  │
│  └────────────────────┘                └─────────────┬──────────────┘  │
└──────────────────────────────────────────────────────┼─────────────────┘
                                                       │ SSH_AUTH_SOCK
                                                       │ (Named Pipe / Unix Socket)
                                                       ▼
                                         ┌───────────────────────────┐
                                         │  ssh Client (OpenSSH)     │
                                         └───────────────────────────┘
```

1. **Vault Sync & Key Decryption**: On host connection, the required SSH private key is decrypted on-demand into memory.
2. **In-Process Agent**: WardenSSH serves an `ssh-agent` protocol server on a local pipe/socket (`\\.\pipe\wardenssh-agent-<pid>` on Windows, unix socket on Linux/macOS).
3. **Execution**: WardenSSH sets `SSH_AUTH_SOCK` and spawns `ssh`, enabling public-key authentication without writing key files to the filesystem.

---

## 🚀 Installation

### Prerequisites

- **Go 1.22+**
- OpenSSH client (`ssh`) installed and available in your `PATH`.

### Installing via `go install`

```bash
go install github.com/ac-kurniawan/wardenssh@latest
```

Alternatively, clone the repository and build manually:

```bash
git clone https://github.com/ac-kurniawan/wardenssh.git
cd wardenssh
go build -o wardenssh .
```

---

## 🏁 Quick Start

Launch WardenSSH by running:

```bash
wardenssh
```

On first launch, WardenSSH will guide you through setting up your Bitwarden or Vaultwarden vault connections.

### `--no-keyring` Fallback

If running in a headless environment without an OS keyring service available, use the `--no-keyring` flag to prompt for master passwords interactively on launch:

```bash
wardenssh --no-keyring
```

---

## ⚙️ Configuration

Configuration is stored in `~/.ssh/wardenssh.json`. This file contains **only non-sensitive preferences and connection parameters**. **No secrets or tokens are stored in this file.**

```json
{
  "vaults": [
    {
      "name": "personal",
      "server": "https://vw.example.com",
      "email": "user@example.com"
    },
    {
      "name": "work",
      "server": "https://vaultwarden.company.com",
      "email": "employee@company.com"
    }
  ],
  "custom_fields": {
    "host": "host",
    "user": "user",
    "port": "port",
    "proxyjump": "proxyjump"
  },
  "keyring": true
}
```

### Bitwarden Vault Item Structure

To have an SSH Key item from Bitwarden/Vaultwarden appear in your WardenSSH host list:
1. Create an item of type **SSH Key** (Type 5) in your vault.
2. Store your private key in the standard `SSH Key` field.
3. Add a Custom Field named `host` (value: target hostname or IP address).
4. *(Optional)* Add Custom Fields for `user`, `port`, and `proxyjump`.

---

## ⌨️ Controls & Shortcuts

| Key / Shortcut | Action |
| :--- | :--- |
| `↑` / `↓` or `k` / `j` | Navigate host list |
| `Enter` | Connect to selected host / Switch to live session |
| `/` | Focus search / fuzzy filter input |
| `Tab` | Cycle source filter scope (`All` → `Vaults` → `~/.ssh/config`) |
| `Esc` | Clear filter / Return to host list from active session |
| `Ctrl + Q` | Open exit confirmation modal |

---

## 🔒 Security Model

WardenSSH is built with strict zero-trust local hygiene:

- **Zero-Disk-Footprint**: Private keys reside only in vault storage at rest and in volatile process RAM during active sessions. Keys are never written to `~/.ssh/`, `/tmp`, or any local disk file.
- **In-Memory Passphrases**: Key passphrases are prompted interactively and cached only in session memory.
- **Secure Keyring Auth**: Vault session refresh tokens are saved in OS-level credential stores (Windows Credential Manager, macOS Keychain, Linux Secret Service).
- **Read-Only SSH Config**: WardenSSH parses `~/.ssh/config` as a read-only data source and never modifies your local SSH configuration files.
- **No Disk Logging**: Diagnostics and errors are written strictly to `stderr`. No key material, hostnames, or connection metadata are logged to disk.

---

## 🌐 Cross-Platform Support

| OS | Agent Socket / Pipe | OS Keyring Backend | PTY Engine |
| :--- | :--- | :--- | :--- |
| **Linux** | Unix Domain Socket | Secret Service API / libsecret | `creack/pty` |
| **macOS** | Unix Domain Socket | macOS Keychain | `creack/pty` |
| **Windows** | Named Pipe (`\\.\pipe\...`) | Windows Credential Manager | ConPTY |

---

## 📄 License

This project is licensed under the [MIT License](LICENSE).