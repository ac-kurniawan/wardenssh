# AGENTS.md

Guidance for OpenCode agents working in this repo.

## Project

WardenSSH — cross-platform (Linux/macOS/Windows) SSH management TUI with
BitWarden/VaultWarden backend integration. Full spec: `.local/spec.md`,
`.local/plan.md`, `.local/story.md`.

- Language: Go
- TUI stack: Bubble Tea, Bubbles, Lip Gloss (Charm)
- Single binary: `go install wardenssh` — no external runtime deps (NOT `bw`
  CLI, NOT the OS ssh-agent service)
- Config: `~/.ssh/wardenssh.json` (non-secret only; vault tokens in OS keyring)
- README is in Indonesian; keep user-facing docs consistent unless asked.

## Critical Architectural Constraints (DO NOT VIOLATE)

- **Private keys never written to disk.** RAM + vault at rest only. Never
  write a key to `~/.ssh/`, a temp file, or a cache file.
- **No file logging.** Diagnostics to stderr only. Never log key bytes,
  passphrases, tokens, or connection metadata (hostnames reveal infra).
- **No secrets in `~/.ssh/wardenssh.json`.** Refresh tokens live in the OS
  keyring (`go-keyring`), never in the config file.
- **In-process everything.** The TUI binary IS the ssh-agent (serves the
  agent protocol on its own pipe) AND the vault client. No daemon, no
  subprocess CLI orchestration. The TUI process must stay alive for the
  duration of any ssh session (the agent lives inside it).
- **`~/.ssh/` is read-only in v0.** WardenSSH reads `~/.ssh/config` and
  existing `~/.ssh/id_*`; it never writes there.

## The SDK Trap

`github.com/bitwarden/sdk-go` is the **Bitwarden Secrets Manager** SDK
(machine accounts, `AccessTokenLogin`, `Projects()`/`Secrets()`). It does
NOT cover the **Password Manager** personal vault (master password, 2FA,
cipher items, SSH-Key item type). WardenSSH needs the Password Manager API —
the one `bw` CLI uses. A native client must use a community lib or reimplement
the crypto (PBKDF2/Argon2, HKDF, AES-256-CBC, HMAC, RSA key unwrap). See
Spike #2.

## Development — Workflow (ENFORCED)

- **TDD is mandatory.** Red-Green-Refactor for every change. Write a failing
  test first; write the minimum code to pass; refactor. No untested code
  ships, and no code is written before a test demands it. This includes the
  agent protocol, the crypto path, and the TUI model logic.
- **Commit small, commit often.** One logical change per commit — a single
  failing-then-passing test + its implementation is a commit. Never bundle a
  spike, a refactor, and a feature together. Small commits keep the spikes'
  pass/fail outcome bisectable and keep review tractable.
- **Commits must pass tests.** Every commit on the working branch builds and
  passes `go test ./...` (once `go.mod` exists). A broken commit is a bug —
  fix-forward with a new commit, do not amend the broken one.
- **Do not commit secrets.** No key bytes, passphrases, tokens, vault URLs,
  or real hostnames in test fixtures. Use generated/throwaway values.

## Development — Build Order (SPIKES GATE EVERYTHING)

Do NOT build subsystems until both spikes pass. See `.local/plan.md`.

1. **Spike #1 (hours):** Prove Windows OpenSSH 9.5p2 honors `SSH_AUTH_SOCK`
   pointing at a Go-served named pipe. Hardcoded ed25519 key, real sshd
   connect. Linux/macOS is known-to-work; this is Windows-only risk. **If it
   fails, Windows support is in question** — do not proceed with agent
   subsystem until resolved.
2. **Vertical slice (days):** In-process agent + hardcoded key + one-button
   Bubble Tea "connect to hardcoded host" with suspend-and-exec. Proves
   agent ↔ ssh.exe handshake end-to-end. Windows: ConPTY; *nix: `creack/pty`.
3. **Spike #2 (hours–days):** Native BitWarden Password Manager auth +
   decrypt one SSH-Key item. Verify byte-identical against `bw` CLI output.
   **If no viable crypto path, scope must be reduced.**
4. Then: agent (full) → vault client → TUI → config.

## Development — Dependencies

- TUI: `github.com/charmbracelet/bubbletea`, `bubbles`, `lipgloss`
- ssh_config parse: `github.com/kevinburke/ssh_config` (do NOT hand-roll —
  the grammar has 20+ edge cases: wildcards, `Match`, `Include` recursion)
- Keyring: `github.com/zalando/go-keyring` or `github.com/99designs/keyring`
  (cross-platform: Windows Credential Manager, macOS Keychain, Linux Secret
  Service; headless Linux has no agent — `--no-keyring` fallback)
- Crypto: `golang.org/x/crypto/ssh` (`ParseRawPrivateKey`, agent protocol),
  `crypto/ed25519`, `crypto/rsa`, `crypto/ecdsa`, PBKDF2/Argon2/HKDF/
  AES-CBC/HMAC (stdlib + `golang.org/x/crypto`)
- PTY: `github.com/creack/pty` (*nix); ConPTY (Windows, stdlib syscall)

Target: **no CGO**. The bitwarden Secrets Manager SDK needs CGO — we are NOT
using it. Keep the binary pure-Go for cross-compilation.

## Development — Cross-Platform

- **Windows:** ConPTY for PTY; named pipe for agent
  (`\\.\pipe\wardenssh-agent-<pid>`); Credential Manager via go-keyring.
- **macOS:** fork/exec + pty; unix socket for agent; Keychain via go-keyring.
- **Linux:** fork/exec + pty; unix socket for agent; libsecret via go-keyring
  (headless fallback: `--no-keyring` → interactive prompt every launch).

## Testing

- **Agent protocol:** unit test every message type (list, sign, add, remove)
  + malformed/oversized/truncated inputs. The agent is a network server —
  input parsing must not panic or leak.
- **Crypto:** unit test against BitWarden test vectors. Decrypt a known
  encrypted item, verify byte-identical to `bw` CLI output. This is the
  highest-signal-per-line test in the repo.
- **PTY lifecycle:** E2E smoke against a real `sshd` (Docker or local).
  Validates suspend → exec → resume + agent handshake.
- **TUI:** unit test model/Update logic; use Bubble Tea test helpers for
  command assertions.
- **Fuzzing:** deferred to v1+ (agent parser is the eventual fuzz target;
  v0 threat model is "correctness" not "adversarial input" since only
  WardenSSH-spawned ssh talks to the agent).
- **Run:** `go test ./...` (once `go.mod` exists). No test framework beyond
  stdlib `testing`.

## Deployment

- **Install target:** `go install wardenssh` — single pure-Go binary, no
  external runtime dependency, no CGO.
- **Cross-compile targets:** `linux/amd64`, `linux/arm64`, `darwin/amd64`,
  `darwin/arm64`, `windows/amd64`.
- **No service install.** WardenSSH is a user-level TUI, not a daemon. Do
  not create systemd units, Windows services, or launch agents.
- **Runtime prerequisite:** OS keyring available (or `--no-keyring` for
  headless/paranoid). `ssh`/`ssh.exe` on PATH.
- **Config bootstrap:** first run with no `~/.ssh/wardenssh.json` → setup
  modal (vault name, server URL, email). No config written silently.

## Commands (once go.mod exists)

    go build ./...          # compile
    go test ./...           # all tests
    go test -run TestAgent  # agent protocol tests only
    go test -run TestCrypto # crypto vector tests only
    go install              # build + install to GOPATH/bin

No Makefile, no CI, no lint config exists yet. Do not assume one — confirm
before running `golangci-lint` or similar.