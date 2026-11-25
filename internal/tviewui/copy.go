package tviewui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/ac-kurniawan/wardenssh/internal/hosts"
	"github.com/ac-kurniawan/wardenssh/internal/sshconfig"
	"github.com/ac-kurniawan/wardenssh/internal/vault"
	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

// InCopyModal reports whether the copy confirmation modal is open.
func (a *App) InCopyModal() bool { return a.inCopy }

// CopyModal returns the current CopyModal instance (nil when closed).
func (a *App) CopyModal() *CopyModal { return a.copyModal }

// ShowCopyModal opens the copy confirmation modal for a host entry (exported
// for tests; the Ctrl+T binding calls the same code).
func (a *App) ShowCopyModal(entry hosts.Entry) { a.showCopyModal(entry) }

// CloseCopyModal dismisses the copy modal and returns focus to the host list.
func (a *App) CloseCopyModal() {
	if a.inCopy {
		a.inCopy = false
		a.overlay.RemovePage("copy")
		a.hostPane.Refresh()
		a.app.SetFocus(a.hostPane.Primitive())
	}
}

// showCopyModal picks the copy direction from the selected entry's source: a
// local entry (file config) copies into an unlocked vault, a vault entry
// exports its private key to ~/.ssh/config. The modal states exactly what the
// copy touches before anything is written.
func (a *App) showCopyModal(entry hosts.Entry) {
	if a.inCopy || a.inDelete || a.inEdit || a.inCreate {
		return
	}

	if entry.Source == "file" || entry.Source == "~/.ssh/config" {
		target := a.copyVaultTarget()
		if target == "" {
			a.showInfoModal("No unlocked vault available.\n\nAdd or unlock a vault first.")
			return
		}
		a.openCopyModal(entry, "local → vault", target,
			"Local entry and key file are kept.",
			func() { a.runCopy(entry, func() error { return a.HandleCopyLocalToVault(entry, target) }) })
		return
	}

	configPath, _, err := a.localSSHConfig()
	if err != nil {
		a.showInfoModal(fmt.Sprintf("Copy failed.\n\n%v", err))
		return
	}
	a.openCopyModal(entry, "vault → local", configPath,
		"Private key will be written to disk (overrides zero-disk).",
		func() { a.runCopy(entry, func() error { return a.HandleCopyVaultToLocal(entry) }) })
}

func (a *App) openCopyModal(entry hosts.Entry, direction, targetDesc, warning string, onCopy func()) {
	a.inCopy = true
	a.copyModal = NewCopyModal(entry.Alias, direction, targetDesc, warning)
	a.copyModal.SetOnCopy(onCopy)
	a.copyModal.SetOnCancel(a.CloseCopyModal)
	a.overlay.AddPage("copy", a.copyModal.Primitive(), true, true)
	a.app.SetFocus(a.copyModal.Primitive())
}

// runCopy closes the modal, then surfaces any failure in the info overlay so
// the reason is never silently swallowed.
func (a *App) runCopy(entry hosts.Entry, copyFn func() error) {
	a.CloseCopyModal()
	if err := copyFn(); err != nil {
		a.showInfoModal(fmt.Sprintf("Copy '%s' failed.\n\n%v", entry.Alias, err))
	}
}

// copyVaultTarget returns the first unlocked vault source that is not the local
// config ("~/.ssh/config"), or "" when no vault is available.
func (a *App) copyVaultTarget() string {
	if a.deps.VaultCli == nil {
		return ""
	}
	for _, src := range a.deps.VaultCli.Sources() {
		name := src.Name()
		if name == "" || name == "~/.ssh/config" || name == "file" {
			continue
		}
		if a.vaultSourceUnlocked(name) {
			return name
		}
	}
	return ""
}

// vaultSourceUnlocked reports whether name resolves to a source with a live
// vault session.
func (a *App) vaultSourceUnlocked(name string) bool {
	source, sess, _, _, err := a.resolveVaultTarget(name)
	return err == nil && source != nil && sess != nil
}

// HandleCopyVaultToLocal exports a vault host entry to ~/.ssh/config: it
// decrypts the item's private key, writes it to id_<algo>_<alias> (plus .pub)
// and appends the Host block. Existing keys or aliases are never overwritten.
//
// shortcut: this writes a private key to disk on purpose (user chose
// full-export over the zero-disk default). If the vault also gains a
// reference-only export mode, branch here rather than weakening the copy.
func (a *App) HandleCopyVaultToLocal(entry hosts.Entry) error {
	if entry.AuthKind == "password" {
		return fmt.Errorf("password-credential entry has no key to export")
	}
	if a.deps.VaultCli == nil {
		return fmt.Errorf("no vault client available")
	}

	// shortcut: connect.findVaultItem is unexported; duplicated here. Export it
	// if a third caller appears.
	var item vault.Item
	var src vault.Source
	for _, s := range a.deps.VaultCli.Sources() {
		if s.Name() != entry.Source {
			continue
		}
		items, err := s.Items()
		if err != nil {
			continue
		}
		for _, it := range items {
			if it.Name == entry.Alias {
				item, src = it, s
				break
			}
		}
		if src != nil {
			break
		}
	}
	if src == nil {
		return fmt.Errorf("item %q not found in vault %q", entry.Alias, entry.Source)
	}

	privPEM, err := src.DecryptPrivateKey(item, "")
	if err != nil {
		return fmt.Errorf("decrypt private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(privPEM)
	if err != nil {
		return fmt.Errorf("key is not exportable (passphrase-protected keys are unsupported): %w", err)
	}

	configPath, sshDir, err := a.localSSHConfig()
	if err != nil {
		return err
	}
	identityPath := filepath.Join(sshDir, fmt.Sprintf("id_%s_%s", keyAlgoName(signer.PublicKey()), entry.Alias))

	if _, err := os.Stat(identityPath); err == nil {
		return fmt.Errorf("key file %s already exists; refusing to overwrite", identityPath)
	}
	for _, e := range a.hostList.All() {
		if e.Alias == entry.Alias && e.Source == "file" {
			return fmt.Errorf("~/.ssh/config already defines host %q", entry.Alias)
		}
	}

	if err := sshconfig.WriteKeyToFile(identityPath, privPEM); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	cfg := sshconfig.HostConfig{
		Alias:        entry.Alias,
		HostName:     entry.HostName,
		User:         entry.User,
		Port:         entry.Port,
		ProxyJump:    entry.ProxyJump,
		IdentityFile: identityPath,
	}
	if err := sshconfig.AppendHostEntry(configPath, cfg); err != nil {
		// Roll back the key pair we just wrote so a failed config write does
		// not leave an orphaned identity behind.
		_ = os.Remove(identityPath)
		_ = os.Remove(identityPath + ".pub")
		return fmt.Errorf("append config entry: %w", err)
	}

	a.hostList.Merge([]hosts.Entry{{
		Alias:        entry.Alias,
		HostName:     entry.HostName,
		User:         entry.User,
		Port:         entry.Port,
		ProxyJump:    entry.ProxyJump,
		Source:       "file",
		IdentityFile: identityPath,
		AuthKind:     "key",
	}})
	a.hostPane.Refresh()
	return nil
}

// HandleCopyLocalToVault copies a local host entry into the named vault as an
// SSH-Key item. Copy-keep: the local entry and its key file stay untouched.
func (a *App) HandleCopyLocalToVault(entry hosts.Entry, vaultTarget string) error {
	if entry.AuthKind == "password" || entry.IdentityFile == "" {
		return fmt.Errorf("entry %q has no key file to copy", entry.Alias)
	}

	keyPath := entry.IdentityFile
	if strings.HasPrefix(keyPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		keyPath = filepath.Join(home, keyPath[2:])
	}
	privPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read key file %s: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(privPEM)
	if err != nil {
		return fmt.Errorf("key is not copyable (passphrase-protected keys are unsupported): %w", err)
	}

	targetSource, sess, cf, serverURL, err := a.resolveVaultTarget(vaultTarget)
	if err != nil {
		return err
	}

	items, err := targetSource.Items()
	if err != nil {
		return fmt.Errorf("list vault items: %w", err)
	}
	for _, it := range items {
		if it.Name == entry.Alias {
			return fmt.Errorf("vault %q already has an item named %q", targetSource.Name(), entry.Alias)
		}
	}

	encName, customFields, err := buildVaultCipherFields(sess, cf, entry.Alias,
		entry.HostName, entry.User, entry.Port, entry.ProxyJump)
	if err != nil {
		return err
	}
	sshKey, err := encryptSshKey(sess, string(privPEM),
		string(ssh.MarshalAuthorizedKey(signer.PublicKey())),
		ssh.FingerprintSHA256(signer.PublicKey()))
	if err != nil {
		return err
	}

	created, err := vaultclientNew(serverURL).CreateCipher(sess, vaultclient.Cipher{
		Name:   encName,
		Type:   5,
		SshKey: sshKey,
		Fields: customFields,
	})
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}
	if created != nil {
		targetSource.AddCipher(*created)
	}

	a.hostList.Merge([]hosts.Entry{{
		Alias:     entry.Alias,
		HostName:  entry.HostName,
		User:      entry.User,
		Port:      entry.Port,
		ProxyJump: entry.ProxyJump,
		Source:    targetSource.Name(),
		AuthKind:  "key",
	}})
	a.hostPane.Refresh()
	return nil
}

// keyAlgoName maps a public key type to the algo token used in generated
// identity file names (id_<algo>_<alias>).
func keyAlgoName(pub ssh.PublicKey) string {
	switch t := pub.Type(); t {
	case ssh.KeyAlgoRSA:
		return "rsa4096"
	case ssh.KeyAlgoED25519:
		return "ed25519"
	default:
		return strings.TrimPrefix(t, "ssh-")
	}
}

// HandleCopyKeyOpen opens the copy modal for the selected host entry (Ctrl+T).
func (a *App) HandleCopyKeyOpen() {
	if e, ok := a.hostPane.SelectedEntry(); ok {
		a.showCopyModal(e)
	}
}

// ConfirmCopy confirms the open copy modal (tests / modal 'y').
func (a *App) ConfirmCopy() {
	if a.inCopy && a.copyModal != nil {
		a.copyModal.TriggerCopy()
	}
}
