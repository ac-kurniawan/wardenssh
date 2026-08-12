#!/bin/bash
# VaultWarden API helpers using the project's own vaultclient package

VW_URL="${VW_URL:-http://100.110.2.11:8000}"
VW_EMAIL="${VW_EMAIL:-wardenssh-test@example.com}"
VW_PASS="${VW_PASS:-TestMasterPass123!}"

# Build the vault-setup helper binary from inline Go source
build_vault_helper() {
    cat > /home/project/wardenssh/e2e/lib/vault-setup.go << 'GOEOF'
package main

import (
    "fmt"
    "net/http"
    "os"
    "strings"

    "github.com/ac-kurniawan/wardenssh/internal/vaultclient"
    "github.com/ac-kurniawan/wardenssh/internal/vaultcrypto"
)

func main() {
    if len(os.Args) < 5 {
        fmt.Fprintf(os.Stderr, "usage: vault-setup <cmd> <url> <email> <pass> [args...]\n")
        os.Exit(1)
    }
    cmd := os.Args[1]
    vwURL := os.Args[2]
    email := os.Args[3]
    pass := os.Args[4]

    c := vaultclient.New(vwURL)

    switch cmd {
    case "register":
        // Step 1: Prelogin (get KDF params — will fail for non-existent user,
        // but that's ok, we use defaults: PBKDF2, 100000 iterations)
        kdfIter := 100000

        // Step 2: Derive account keys
        ak, err := vaultclient.DeriveAccountKeys(email, pass, kdfIter)
        if err != nil {
            fmt.Fprintf(os.Stderr, "derive keys: %v\n", err)
            os.Exit(1)
        }

        // Step 3: Register
        if err := c.Register(ak, kdfIter); err != nil {
            fmt.Fprintf(os.Stderr, "register: %v\n", err)
            os.Exit(1)
        }
        fmt.Println("OK: account registered")

    case "create-item":
        // Args: create-item <url> <email> <pass> <itemName> <hostIP> <hostUser> <hostPort> <privateKeyPath>
        if len(os.Args) < 10 {
            fmt.Fprintf(os.Stderr, "usage: vault-setup create-item <url> <email> <pass> <itemName> <hostIP> <hostUser> <hostPort> <keyPath>\n")
            os.Exit(1)
        }
        itemName := os.Args[5]
        hostIP := os.Args[6]
        hostUser := os.Args[7]
        hostPort := os.Args[8]
        keyPath := os.Args[9]

        // Login to get session
        sess, err := c.Login(email, pass)
        if err != nil {
            fmt.Fprintf(os.Stderr, "login: %v\n", err)
            os.Exit(1)
        }

        // Read the private key file
        keyBytes, err := os.ReadFile(keyPath)
        if err != nil {
            fmt.Fprintf(os.Stderr, "read key: %v\n", err)
            os.Exit(1)
        }

        // Encrypt all fields using the session's symmetric key
        encName, err := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte(itemName))
        if err != nil { fmt.Fprintf(os.Stderr, "encrypt name: %v\n", err); os.Exit(1) }

        encKey, err := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, keyBytes)
        if err != nil { fmt.Fprintf(os.Stderr, "encrypt key: %v\n", err); os.Exit(1) }

        // Encrypt public key (derive from private key — or just use a placeholder)
        // Read the public key file (same path + .pub)
        pubKeyBytes, err := os.ReadFile(keyPath + ".pub")
        if err != nil {
            fmt.Fprintf(os.Stderr, "read pub key: %v\n", err)
            os.Exit(1)
        }
        encPubKey, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, pubKeyBytes)
        encFingerprint, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte(""))
        encPassphrase, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte(""))

        // Encrypt custom fields
        encHost, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte(hostIP))
        encUser, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte(hostUser))
        encPort, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte(hostPort))
        encFieldNameHost, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte("host"))
        encFieldNameUser, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte("user"))
        encFieldNamePort, _ := vaultcrypto.Encrypt(sess.SymEnc, sess.SymMac, []byte("port"))

        // Build the cipher JSON and POST it
        body := fmt.Sprintf(`{
            "type": 5,
            "name": "%s",
            "notes": null,
            "sshKey": {
                "privateKey": "%s",
                "publicKey": "%s",
                "keyFingerprint": "%s",
                "passphrase": "%s"
            },
            "fields": [
                {"name": "%s", "value": "%s", "type": 0},
                {"name": "%s", "value": "%s", "type": 0},
                {"name": "%s", "value": "%s", "type": 0}
            ]
        }`, encName, encKey, encPubKey, encFingerprint, encPassphrase,
            encFieldNameHost, encHost,
            encFieldNameUser, encUser,
            encFieldNamePort, encPort)

        // POST to /api/ciphers with Bearer token
        req, err := http.NewRequest(http.MethodPost, vwURL+"/api/ciphers", strings.NewReader(body))
        if err != nil { fmt.Fprintf(os.Stderr, "create req: %v\n", err); os.Exit(1) }
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", "Bearer "+sess.AccessToken)

        resp, err := c.HTTP.Do(req)
        if err != nil { fmt.Fprintf(os.Stderr, "create item: %v\n", err); os.Exit(1) }
        defer resp.Body.Close()
        if resp.StatusCode != 200 {
            fmt.Fprintf(os.Stderr, "create item: HTTP %d\n", resp.StatusCode)
            os.Exit(1)
        }
        fmt.Println("OK: SSH-Key item created")

    case "login-test":
        // Just test that login works
        sess, err := c.Login(email, pass)
        if err != nil {
            fmt.Fprintf(os.Stderr, "login: %v\n", err)
            os.Exit(1)
        }
        sr, err := c.Sync(sess)
        if err != nil {
            fmt.Fprintf(os.Stderr, "sync: %v\n", err)
            os.Exit(1)
        }
        fmt.Printf("OK: login + sync succeeded, %d ciphers\n", len(sr.Ciphers))
        for _, ci := range sr.Ciphers {
            nameBytes, _ := sess.DecryptField(ci.Name)
            fmt.Printf("  cipher: id=%s name=%s type=%d sshKey=%v\n", ci.ID, string(nameBytes), ci.Type, ci.SshKey != nil)
        }
    }
}
GOEOF

    cd /home/project/wardenssh
    export PATH=$PATH:/usr/local/go/bin
    go build -o /tmp/vault-setup ./e2e/lib/vault-setup.go
    rm -f /home/project/wardenssh/e2e/lib/vault-setup.go
}
