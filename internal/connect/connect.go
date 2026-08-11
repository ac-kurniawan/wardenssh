// Package connect wires the launcher's ConnectMsg into the full connect flow:
// lazy-decrypt the vault key (Q8/C), load it into the in-process agent
// (Q19/B ref-counted), and spawn ssh via the session manager with
// SSH_AUTH_SOCK pointed at our agent pipe (Q4/B).
package connect

import (
	"fmt"
	"path/filepath"
	"runtime"

	"golang.org/x/crypto/ssh"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/session"
	"github.com/ac-kurniawan/wardenssh/internal/sshagent"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
)

// SSHBin is the ssh executable name (overridable for tests).
var SSHBin = "ssh"

// AgentPipePath returns the named pipe / unix socket path for the agent.
// Overridable for tests.
var AgentPipePath = defaultAgentPipe

// Connector performs the full connect flow for a host entry.
type Connector struct {
	Agent *sshagent.Keyring
	Mgr   *session.Manager
}

// Result is the outcome of a connect attempt.
type Result struct {
	Session *session.Session
	Err     error
}

// Connect performs the full flow:
// 1. Resolve the vault source + decrypt the private key (lazy, Q8/C).
// 2. Parse the key + load into the agent (ref-counted by session ID, Q19/B).
// 3. Build the ssh argv (host, user, port, ProxyJump, SSH_AUTH_SOCK).
// 4. Spawn ssh via the session manager.
// The vaultSources map is keyed by the entry's Source label.
func Connect(entry hosts.Entry, sessionID string, vc vault.Client, c *Connector) Result {
	if c == nil || c.Mgr == nil || c.Agent == nil {
		return Result{Err: fmt.Errorf("connect: connector not initialized")}
	}

	// 1. For file-sourced entries, ssh reads the key from disk directly
	//    (Q6/A read-only ~/.ssh). No agent involvement needed for file keys —
	//    ssh.exe reads IdentityFile from ~/.ssh/config. For vault-sourced
	//    entries, lazy-decrypt the key + load into the agent.
	if entry.Source != "file" {
		if vc == nil {
			return Result{Err: fmt.Errorf("connect: vault entry %q but no vault client", entry.Alias)}
		}
		// Find the source + item matching this entry.
		item, src, err := findVaultItem(vc, entry)
		if err != nil {
			return Result{Err: fmt.Errorf("connect: find vault item: %w", err)}
		}

		// Lazy-decrypt the private key (Q8/C).
		decrypted, err := src.DecryptPrivateKey(item, "")
		if err != nil {
			return Result{Err: fmt.Errorf("connect: decrypt private key: %w", err)}
		}

		// Parse + load into the agent.
		priv, err := ssh.ParseRawPrivateKey(decrypted)
		if err != nil {
			return Result{Err: fmt.Errorf("connect: parse private key: %w", err)}
		}
		if _, err := c.Agent.Load(priv, entry.Alias, sessionID); err != nil {
			return Result{Err: fmt.Errorf("connect: agent load: %w", err)}
		}
	}

	// 3. Build ssh argv.
	argv := SSHArgv(entry, AgentPipePath())

	// 4. Spawn ssh via the session manager with SSH_AUTH_SOCK.
	env := EnvForAgent(AgentPipePath())
	sess, err := c.Mgr.SpawnWithEnv(entry.Alias, entry.Source, argv, env)
	return Result{Session: sess, Err: err}
}

// SSHArgv builds the ssh command-line arguments for a host entry. The agent
// pipe is passed via SSH_AUTH_SOCK (set as env by the session manager caller).
// For vault-sourced entries, NO -i is passed (ssh uses the agent). For
// file-sourced entries with an IdentityFile, it's read from ~/.ssh/config by
// ssh directly.
func SSHArgv(entry hosts.Entry, agentPipe string) []string {
	var args []string
	args = append(args, SSHBin)

	// Batch mode — no interactive prompts (the agent handles auth).
	args = append(args, "-o", "BatchMode=yes")
	// Accept new host keys (per Q9/A vertical-slice pattern).
	args = append(args, "-o", "StrictHostKeyChecking=accept-new")

	// Port (if specified).
	if entry.Port != "" {
		args = append(args, "-p", entry.Port)
	}

	// ProxyJump (if specified).
	if entry.ProxyJump != "" {
		args = append(args, "-J", entry.ProxyJump)
	}

	// Target: user@host or just host.
	target := entry.HostName
	if entry.User != "" {
		target = entry.User + "@" + entry.HostName
	}
	args = append(args, target)

	return args
}

// EnvForAgent returns the environment variables needed for ssh to find our
// agent pipe (SSH_AUTH_SOCK). The caller appends these to the child's env.
func EnvForAgent(agentPipe string) []string {
	return []string{"SSH_AUTH_SOCK=" + agentPipe}
}

// findVaultItem locates the vault.Item + vault.Source matching a host entry
// by alias (item Name) + source label.
func findVaultItem(vc vault.Client, entry hosts.Entry) (vault.Item, vault.Source, error) {
	for _, src := range vc.Sources() {
		if src.Name() != entry.Source {
			continue
		}
		items, err := src.Items()
		if err != nil {
			continue
		}
		for _, it := range items {
			if it.Name == entry.Alias {
				return it, src, nil
			}
		}
	}
	return vault.Item{}, nil, fmt.Errorf("item %q not found in source %q", entry.Alias, entry.Source)
}

// defaultAgentPipe returns a platform-appropriate agent pipe path.
func defaultAgentPipe() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\wardenssh-agent`
	}
	// Unix: use a stable path in the user's runtime dir.
	return filepath.Join("/tmp", "wardenssh-agent.sock")
}