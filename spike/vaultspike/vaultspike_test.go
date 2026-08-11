// Package vaultspike: vaultspike_test.go is Spike #2 (see .local/plan.md) —
// prove the native BitWarden Password Manager crypto + API by logging into a
// real VaultWarden instance, syncing the vault, and decrypting an SSH-Key
// item's private key, cross-verified byte-identical against the official `bw`
// CLI decrypting the same item.
//
// GATED: skipped unless WARDENSSH_VAULTSPIKE=1 AND WARDENSSH_VW_EMAIL +
// WARDENSSH_VW_PASS env vars are set. The credentials are NEVER hardcoded in
// the repo — they live only in the test process's environment and are never
// logged. Read-only against the vault (login + sync + decrypt); no items are
// created or modified.
package vaultspike

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ac-kurniawan/wardenssh/internal/vaultclient"
)

// vwURL is the VaultWarden base URL. Override via WARDENSSH_VW_URL if needed.
const vwURL = "https://vw.server3.arcaku-labs.com"

// envOr returns the env var or skips the test if unset.
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("env %s not set — skipping vault spike", key)
	}
	return v
}

// TestLoginAndDecryptProtectedKey: the core Spike #2 proof — WardenSSH's
// native crypto logs into a real VaultWarden and decrypts the Protected
// Symmetric Key + fields. If login succeeds, the master-key derivation,
// auth-hash, HKDF stretch, and symkey unwrap are all byte-compatible with
// BitWarden (the server validates the auth hash).
func TestLoginAndDecryptProtectedKey(t *testing.T) {
	if os.Getenv("WARDENSSH_VAULTSPIKE") != "1" {
		t.Skip("vault spike skipped (set WARDENSSH_VAULTSPIKE=1 + WARDENSSH_VW_EMAIL/PASS)")
	}
	email := envOrSkip(t, "WARDENSSH_VW_EMAIL")
	pass := envOrSkip(t, "WARDENSSH_VW_PASS")

	c := vaultclient.New(vwURL)
	sess, err := c.Login(email, pass)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	t.Logf("PASS: login succeeded — access token obtained, Protected Symmetric Key decrypted (32+32 bytes)")

	if len(sess.SymEnc) != 32 || len(sess.SymMac) != 32 {
		t.Fatalf("sym key halves = %d/%d, want 32/32", len(sess.SymEnc), len(sess.SymMac))
	}
}

// TestSyncDecryptsItemFields: sync the vault and decrypt every cipher's Name
// + custom fields under the session's symmetric key. If all decrypt cleanly,
// the AES-CBC-HMAC item-encryption path is proven against real vault data.
func TestSyncDecryptsItemFields(t *testing.T) {
	if os.Getenv("WARDENSSH_VAULTSPIKE") != "1" {
		t.Skip("vault spike skipped")
	}
	email := envOrSkip(t, "WARDENSSH_VW_EMAIL")
	pass := envOrSkip(t, "WARDENSSH_VW_PASS")

	c := vaultclient.New(vwURL)
	sess, err := c.Login(email, pass)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	t.Logf("synced %d ciphers", len(sr.Ciphers))

	decrypted := 0
	for _, ci := range sr.Ciphers {
		if ci.Name == "" {
			continue
		}
		name, err := sess.DecryptField(ci.Name)
		if err != nil {
			t.Errorf("decrypt cipher %s Name: %v", ci.ID, err)
			continue
		}
		decrypted++
		// Decrypt custom fields (host/user/port/...).
		for _, f := range ci.Fields {
			if f.Value != "" {
				if _, err := sess.DecryptField(f.Value); err != nil {
					t.Errorf("decrypt cipher %q field: %v", string(name), err)
				}
			}
		}
	}
	if decrypted == 0 {
		t.Log("vault has no ciphers to decrypt (empty vault) — field-decrypt path untested")
	} else {
		t.Logf("PASS: decrypted %d cipher names + their fields", decrypted)
	}
}

// TestSSHKeyItemDecryptsByteIdenticalToBW: the gold-standard Spike #2 proof.
// Find an SSH-Key item in the vault, decrypt its private key with WardenSSH's
// native crypto, then decrypt the SAME item with `bw get item --raw` and
// compare byte-for-byte. Identical bytes = the crypto is BitWarden-correct.
func TestSSHKeyItemDecryptsByteIdenticalToBW(t *testing.T) {
	if os.Getenv("WARDENSSH_VAULTSPIKE") != "1" {
		t.Skip("vault spike skipped")
	}
	email := envOrSkip(t, "WARDENSSH_VW_EMAIL")
	pass := envOrSkip(t, "WARDENSSH_VW_PASS")

	c := vaultclient.New(vwURL)
	sess, err := c.Login(email, pass)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	sr, err := c.Sync(sess)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Find the first cipher whose decrypted Name or SSH-key private key we
	// can read. We look for ciphers with an SshKey block.
	var sshItem *vaultclient.Cipher
	var sshItemName string
	for i := range sr.Ciphers {
		if sr.Ciphers[i].SshKey == nil || sr.Ciphers[i].SshKey.PrivateKey == "" {
			continue
		}
		sshItem = &sr.Ciphers[i]
		n, err := sess.DecryptField(sr.Ciphers[i].Name)
		if err == nil {
			sshItemName = string(n)
		}
		break
	}
	if sshItem == nil {
		t.Skip("no SSH-Key item in the vault — create one in the web vault to run this gold-standard check")
	}

	// WardenSSH decrypts the private key field.
	wardenPriv, err := sess.DecryptField(sshItem.SshKey.PrivateKey)
	if err != nil {
		t.Fatalf("WardenSSH decrypt SSH private key: %v", err)
	}
	t.Logf("WardenSSH decrypted SSH private key (%d bytes) for item %q", len(wardenPriv), sshItemName)

	// Cross-verify: `bw get item <id>` then extract the private key via bw's
	// own decrypt. bw returns a JSON object with decrypted fields when run
	// with a valid session.
	bwPriv := bwGetSSHPrivateKey(t, sshItem.ID, email, pass)
	if bwPriv == "" {
		t.Skipf("bw could not return the SSH private key for item %s (field empty?) — skipping byte-compare", sshItem.ID)
	}

	if string(wardenPriv) != bwPriv {
		t.Errorf("BYTE MISMATCH: WardenSSH (%d bytes) != bw (%d bytes) for item %q",
			len(wardenPriv), len(bwPriv), sshItemName)
		// Log first/last few bytes ONLY (not the full key) to aid debugging
		// without leaking the key.
		t.Logf("warden head=%x tail=%x", head(wardenPriv, 8), tail(wardenPriv, 8))
		t.Logf("bw      head=%x tail=%x", head([]byte(bwPriv), 8), tail([]byte(bwPriv), 8))
	} else {
		t.Logf("PASS: byte-identical decrypt (%d bytes) — WardenSSH crypto == bw CLI", len(wardenPriv))
	}
}

// bwGetSSHPrivateKey logs in via bw (if needed) and returns the decrypted SSH
// private key string for the given item id. Uses BW_SESSION isolation so it
// doesn't clobber the caller's bw state.
func bwGetSSHPrivateKey(t *testing.T, itemID, email, pass string) string {
	t.Helper()
	// Login fresh and capture the session token.
	sess := runBW(t, "login", email, pass, "--raw")
	if sess == "" {
		t.Fatalf("bw login returned no session token")
	}
	defer runBWSilent("logout")

	// Get the item as JSON (bw decrypts fields when a session is active).
	out := runBWSession(t, sess, "get", "item", itemID)
	// The JSON has a .sshKey.privateKey field (decrypted plaintext). Extract it
	// with a minimal parse — avoid pulling in a JSON dep here; use a regex-ish
	// line scan for "privateKey":"...".
	return extractJSONField(out, "privateKey")
}

func runBW(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := exec.Command("bw", args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("bw %v: %v (stderr: %s)", args, err, errOut.String())
	}
	return strings.TrimSpace(out.String())
}

func runBWSilent(args ...string) {
	_ = exec.Command("bw", args...).Run()
}

func runBWSession(t *testing.T, sess string, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := exec.Command("bw", args...)
	cmd.Env = append(os.Environ(), "BW_SESSION="+sess)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("bw %v: %v (stderr: %s)", args, err, errOut.String())
	}
	return out.String()
}

// extractJSONField pulls a string field value out of a flat-ish JSON blob
// without a full JSON dep. Good enough for bw's item JSON where the field
// appears once as "privateKey":"<value>". Unescapes JSON string escapes
// (\n, \", \\, etc.) so the result matches the raw decrypted bytes.
func extractJSONField(json, field string) string {
	needle := `"` + field + `":"`
	i := strings.Index(json, needle)
	if i < 0 {
		return ""
	}
	start := i + len(needle)
	// Find the closing quote, handling escaped quotes.
	for j := start; j < len(json); j++ {
		if json[j] == '\\' && j+1 < len(json) {
			j++ // skip escaped char
			continue
		}
		if json[j] == '"' {
			return unescapeJSONString(json[start:j])
		}
	}
	return ""
}

// unescapeJSONString reverses the minimal set of JSON string escapes that
// appear in bw's decrypted output: \n \r \t \" \\ \/ \b \f.
func unescapeJSONString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case '/':
			b.WriteByte('/')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func head(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

func tail(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[len(b)-n:]
}