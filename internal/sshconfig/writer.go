package sshconfig

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// HostConfig holds parameters to append to an ssh_config file.
type HostConfig struct {
	Alias        string
	HostName     string
	User         string
	Port         string
	ProxyJump    string
	IdentityFile string
}

// GenerateKeyPair generates an SSH keypair (ed25519 or rsa 4096) in RAM, returning
// the private key PEM string, authorized public key string, and the OpenSSH
// SHA256 fingerprint ("SHA256:..."). The fingerprint is what BitWarden stores
// as the SSH-Key item's keyFingerprint; sending an empty one makes VaultWarden
// null the entire sshKey object.
func GenerateKeyPair(algo string) (string, string, string, error) {
	var pubKey ssh.PublicKey
	var pemBlock *pem.Block

	switch strings.ToLower(algo) {
	case "rsa", "rsa4096":
		rsaKey, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return "", "", "", fmt.Errorf("generate rsa key: %w", err)
		}
		pubKey, err = ssh.NewPublicKey(&rsaKey.PublicKey)
		if err != nil {
			return "", "", "", fmt.Errorf("ssh public key from rsa: %w", err)
		}
		pemBlock, err = ssh.MarshalPrivateKey(rsaKey, "")
		if err != nil {
			return "", "", "", fmt.Errorf("marshal rsa private key: %w", err)
		}
	default: // "ed25519"
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", "", "", fmt.Errorf("generate ed25519 key: %w", err)
		}
		pubKey, err = ssh.NewPublicKey(pub)
		if err != nil {
			return "", "", "", fmt.Errorf("ssh public key from ed25519: %w", err)
		}
		pemBlock, err = ssh.MarshalPrivateKey(priv, "")
		if err != nil {
			return "", "", "", fmt.Errorf("marshal ed25519 private key: %w", err)
		}
	}

	privBytes := pem.EncodeToMemory(pemBlock)
	pubBytes := ssh.MarshalAuthorizedKey(pubKey)
	return string(privBytes), string(pubBytes), ssh.FingerprintSHA256(pubKey), nil
}

// GenerateKeyToFile generates an SSH keypair (ed25519 or rsa 4096) and writes
// the private key to keyPath (0600) and public key to keyPath + ".pub" (0644).
func GenerateKeyToFile(algo string, keyPath string) error {
	privPEM, pubAuth, _, err := GenerateKeyPair(algo)
	if err != nil {
		return err
	}

	if dir := filepath.Dir(keyPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create key directory: %w", err)
		}
	}

	if err := os.WriteFile(keyPath, []byte(privPEM), 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	if err := os.WriteFile(keyPath+".pub", []byte(pubAuth), 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	return nil
}

// AppendHostEntry appends a formatted Host block to the ssh config file.
func AppendHostEntry(configPath string, cfg HostConfig) error {
	if dir := filepath.Dir(configPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
	}

	f, err := os.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\nHost %s\n", cfg.Alias))
	if cfg.HostName != "" {
		sb.WriteString(fmt.Sprintf("    HostName %s\n", cfg.HostName))
	}
	if cfg.User != "" {
		sb.WriteString(fmt.Sprintf("    User %s\n", cfg.User))
	}
	if cfg.Port != "" {
		sb.WriteString(fmt.Sprintf("    Port %s\n", cfg.Port))
	}
	if cfg.ProxyJump != "" {
		sb.WriteString(fmt.Sprintf("    ProxyJump %s\n", cfg.ProxyJump))
	}
	if cfg.IdentityFile != "" {
		sb.WriteString(fmt.Sprintf("    IdentityFile %s\n", cfg.IdentityFile))
	}

	if _, err := f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("write config entry: %w", err)
	}

	return nil
}

// isManagedDirective reports whether key is one of the 5 fields managed by WardenSSH.
func isManagedDirective(key string) bool {
	switch strings.ToLower(key) {
	case "hostname", "user", "port", "proxyjump", "identityfile":
		return true
	default:
		return false
	}
}

// UpdateHostEntry updates a Host block matching oldAlias in-place, replacing
// managed directives (HostName, User, Port, ProxyJump, IdentityFile) with values
// from cfg, while preserving position, comments, and unmanaged directives.
func UpdateHostEntry(configPath string, oldAlias string, cfg HostConfig) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	targetAlias := strings.ToLower(oldAlias)

	var newLines []string
	found := false
	inTargetBlock := false
	var preservedLines []string

	flushUpdatedBlock := func() {
		newLines = append(newLines, fmt.Sprintf("Host %s", cfg.Alias))
		if cfg.HostName != "" {
			newLines = append(newLines, fmt.Sprintf("    HostName %s", cfg.HostName))
		}
		if cfg.User != "" {
			newLines = append(newLines, fmt.Sprintf("    User %s", cfg.User))
		}
		if cfg.Port != "" {
			newLines = append(newLines, fmt.Sprintf("    Port %s", cfg.Port))
		}
		if cfg.ProxyJump != "" {
			newLines = append(newLines, fmt.Sprintf("    ProxyJump %s", cfg.ProxyJump))
		}
		if cfg.IdentityFile != "" {
			newLines = append(newLines, fmt.Sprintf("    IdentityFile %s", cfg.IdentityFile))
		}
		newLines = append(newLines, preservedLines...)
		preservedLines = nil
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)

		if len(fields) >= 1 && (strings.EqualFold(fields[0], "Host") || strings.EqualFold(fields[0], "Match")) {
			if inTargetBlock {
				flushUpdatedBlock()
				inTargetBlock = false
			}

			if strings.EqualFold(fields[0], "Host") && len(fields) >= 2 {
				matched := false
				for _, h := range fields[1:] {
					if strings.ToLower(h) == targetAlias {
						matched = true
						break
					}
				}
				if matched {
					found = true
					inTargetBlock = true
					continue
				}
			}
		}

		if inTargetBlock {
			if len(fields) > 0 && isManagedDirective(fields[0]) {
				continue
			}
			preservedLines = append(preservedLines, line)
		} else {
			newLines = append(newLines, line)
		}
	}

	if inTargetBlock {
		flushUpdatedBlock()
	}

	if !found {
		return fmt.Errorf("host %q not found in config", oldAlias)
	}

	output := strings.Join(newLines, "\n")
	if err := os.WriteFile(configPath, []byte(output), 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

func DeleteHostEntry(configPath string, alias string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var newLines []string
	skipping := false

	targetAlias := strings.ToLower(alias)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "Host") {
			matched := false
			for _, h := range fields[1:] {
				if strings.ToLower(h) == targetAlias {
					matched = true
					break
				}
			}
			skipping = matched
		}

		if !skipping {
			newLines = append(newLines, line)
		}
	}

	output := strings.Join(newLines, "\n")
	if err := os.WriteFile(configPath, []byte(output), 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}
