# WARDENSSH

__SSH Management TUI dengan integrasi BitWarden/VaultWarden__

WardenSSH adalah TUI cross-platform (Linux, macOS, Windows) yang memanage koneksi
SSH sekaligus private key — private key **tidak pernah menulis ke disk**. Key
disimpan di vault (BitWarden/VaultWarden) saat rest, dan di RAM saat dipakai,
dikirim ke `ssh` melalui ssh-agent in-process.

Dibangun menggunakan [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Bubbles](https://github.com/charmbracelet/bubbles), dan
[Lip Gloss](https://github.com/charmbracelet/lipgloss).

## Fitur

- Launcher host SSH dengan multi-source: vault (bisa lebih dari satu) + `~/.ssh/config`
- Private key tetap di vault, hanya materialize di RAM via in-process ssh-agent
- Multi sesi paralel (yield-and-switch, bukan terminal multiplexer)
- Filter fuzzy + scope per-source
- Auth vault via OS keyring (Credential Manager / Keychain / libsecret), atau
  `--no-keyring` untuk prompt interaktif setiap launch
- 2FA TOTP & Email
- Passphrase protected keys didukung (prompt interaktif, cache hanya di RAM)
- Bekerja offline secara graceful (vault down → host vault tidak tersedia,
  host `~/.ssh/config` tetap)

## Install

    go install wardenssh

Lalu jalankan `wardenssh`. Konfigurasi ada di `~/.ssh/wardenssh.json`.

## Config

    {
      "vaults": [
        { "name": "personal", "server": "https://vw.example.com", "email": "me@…" }
      ],
      "custom_fields": { "host": "host", "user": "user", "port": "port", "proxyjump": "proxyjump" },
      "keyring": true
    }

Hanya preferensi + parameter koneksi. Tidak ada secret di file ini — refresh
token disimpan di OS keyring.

## Status

Early init. Lihat `.local/spec.md`, `.local/plan.md`, `.local/story.md` untuk
spesifikasi, rencana implementasi, dan user stories.