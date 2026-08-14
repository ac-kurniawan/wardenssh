// Package connect wires the launcher's ConnectMsg into the full connect flow:
// lazy-decrypt the vault key (Q8/C), load it into the in-process agent
// (Q19/B ref-counted), and spawn ssh via the session manager with
// SSH_AUTH_SOCK pointed at our agent pipe (Q4/B).
package connect

import (
	"fmt"
	"os"
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

// askpassExecutable returns the path ssh uses to spawn the SSH_ASKPASS helper
// (the warden binary itself in askpass mode). Overridable for tests.
var askpassExecutable = func() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "wardenssh"
}

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

	fmt.Fprintf(os.Stderr, "wardenssh: connecting to %q (source=%s)\n", entry.Alias, entry.Source)

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
			fmt.Fprintf(os.Stderr, "wardenssh: find vault item: %v\n", err)
			return Result{Err: fmt.Errorf("connect: find vault item: %w", err)}
		}

		// Lazy-decrypt the private key (Q8/C).
		decrypted, err := src.DecryptPrivateKey(item, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "wardenssh: decrypt private key: %v\n", err)
			return Result{Err: fmt.Errorf("connect: decrypt private key: %w", err)}
		}
		fmt.Fprintf(os.Stderr, "wardenssh: decrypted key (%d bytes)\n", len(decrypted))

		// Parse + load into the agent.
		priv, err := ssh.ParseRawPrivateKey(decrypted)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wardenssh: parse private key: %v\n", err)
			return Result{Err: fmt.Errorf("connect: parse private key: %w", err)}
		}
		if _, err := c.Agent.Load(priv, entry.Alias, sessionID); err != nil {
			fmt.Fprintf(os.Stderr, "wardenssh: agent load: %v\n", err)
			return Result{Err: fmt.Errorf("connect: agent load: %w", err)}
		}
		fmt.Fprintf(os.Stderr, "wardenssh: key loaded into agent\n")
	}

	// 3. Build ssh argv.
	argv := SSHArgv(entry, AgentPipePath())
	fmt.Fprintf(os.Stderr, "wardenssh: spawning ssh: %v\n", argv)

	// 4. Spawn ssh via the session manager with SSH_AUTH_SOCK.
	env := EnvForAgent(AgentPipePath())
	sess, err := c.Mgr.SpawnWithEnv(entry.Alias, entry.Source, argv, env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wardenssh: spawn: %v\n", err)
	}
	return Result{Session: sess, Err: err}
}

// PrepareAgentKey decrypts the private key for a vault-sourced entry and loads it
// into the agent keyring for sessionID. No-op for file-sourced entries.
func PrepareAgentKey(entry hosts.Entry, sessionID string, vc vault.Client, agent *sshagent.Keyring) error {
	if entry.Source == "file" {
		return nil
	}
	if vc == nil || agent == nil {
		return fmt.Errorf("prepare key: vault client or agent keyring is nil")
	}
	item, src, err := findVaultItem(vc, entry)
	if err != nil {
		return fmt.Errorf("find vault item: %w", err)
	}
	decrypted, err := src.DecryptPrivateKey(item, "")
	if err != nil {
		return fmt.Errorf("decrypt private key: %w", err)
	}
	priv, err := ssh.ParseRawPrivateKey(decrypted)
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}
	if _, err := agent.Load(priv, entry.Alias, sessionID); err != nil {
		return fmt.Errorf("agent load: %w", err)
	}
	return nil
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

	// For file-sourced entries, pass the IdentityFile directly.
	// Vault-sourced entries use the in-process agent (no -i needed).
	if entry.Source == "file" && entry.IdentityFile != "" {
		args = append(args, "-i", entry.IdentityFile)
	}

	// Port (if specified).
	if entry.Port != "" {
		args = append(args, "-p", entry.Port)
	}

	// ProxyJump (if specified).
	if entry.ProxyJump != "" {
		args = append(args, "-J", entry.ProxyJump)
	}

	// Target: user@host. Default user is "root" when not specified.
	user := entry.User
	if user == "" {
		user = "root"
	}
	target := user + "@" + entry.HostName
	args = append(args, target)

	return args
}

// EnvForAgent returns the environment variables needed for ssh to find our
// agent pipe (SSH_AUTH_SOCK). The caller appends these to the child's env.
func EnvForAgent(agentPipe string) []string {
	return []string{"SSH_AUTH_SOCK=" + agentPipe}
}

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