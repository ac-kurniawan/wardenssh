package vaultclient

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateCipher_Success_SshKey(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedAuth string
	var capturedContentType string
	var capturedBody Cipher

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedContentType = r.Header.Get("Content-Type")

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(bodyBytes, &capturedBody); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := Cipher{
			ID:   "cipher-id-abc",
			Name: capturedBody.Name,
			Type: capturedBody.Type,
			SshKey: capturedBody.SshKey,
			Fields: capturedBody.Fields,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "test-token-123"}
	sshKeyItem := Cipher{
		Name: "2.enc_name==",
		Type: 5,
		SshKey: &SshKey{
			PrivateKey: "2.enc_privkey==",
			PublicKey:  "2.enc_pubkey==",
		},
		Fields: []CustomField{
			{Name: "2.enc_host==", Value: "2.enc_192.168.1.100==", Type: 0},
			{Name: "2.enc_port==", Value: "2.enc_22==", Type: 0},
		},
	}

	created, err := c.CreateCipher(sess, sshKeyItem)
	if err != nil {
		t.Fatalf("CreateCipher failed: %v", err)
	}

	if capturedMethod != http.MethodPost {
		t.Errorf("expected POST method, got %s", capturedMethod)
	}
	if capturedPath != "/api/ciphers" {
		t.Errorf("expected path /api/ciphers, got %s", capturedPath)
	}
	if capturedAuth != "Bearer test-token-123" {
		t.Errorf("expected Authorization header 'Bearer test-token-123', got %s", capturedAuth)
	}
	if capturedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", capturedContentType)
	}
	if capturedBody.Name != "2.enc_name==" || capturedBody.Type != 5 {
		t.Errorf("captured body mismatch: %+v", capturedBody)
	}

	if created == nil {
		t.Fatal("expected non-nil created Cipher")
	}
	if created.ID != "cipher-id-abc" {
		t.Errorf("expected ID 'cipher-id-abc', got %s", created.ID)
	}
	if created.Type != 5 {
		t.Errorf("expected Type 5, got %d", created.Type)
	}
	if created.SshKey == nil || created.SshKey.PrivateKey != "2.enc_privkey==" {
		t.Errorf("expected SshKey.PrivateKey '2.enc_privkey==', got %+v", created.SshKey)
	}
	if len(created.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(created.Fields))
	}
}

func TestCreateCipher_Success_Login(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		resp := Cipher{
			ID:   "login-cipher-456",
			Name: "2.enc_login_name==",
			Type: 1,
			Login: &Login{
				Username: "2.enc_user==",
				Password: "2.enc_pass==",
			},
			Fields: []CustomField{
				{Name: "2.enc_host==", Value: "2.enc_example.com==", Type: 0},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "token-abc"}
	loginItem := Cipher{
		Name: "2.enc_login_name==",
		Type: 1,
		Login: &Login{
			Username: "2.enc_user==",
			Password: "2.enc_pass==",
		},
		Fields: []CustomField{
			{Name: "2.enc_host==", Value: "2.enc_example.com==", Type: 0},
		},
	}

	created, err := c.CreateCipher(sess, loginItem)
	if err != nil {
		t.Fatalf("CreateCipher failed: %v", err)
	}
	if created == nil || created.ID != "login-cipher-456" {
		t.Fatalf("unexpected created cipher: %+v", created)
	}
	if created.Type != 1 {
		t.Errorf("expected Type 1, got %d", created.Type)
	}
	if created.Login == nil || created.Login.Username != "2.enc_user==" || created.Login.Password != "2.enc_pass==" {
		t.Errorf("unexpected login object: %+v", created.Login)
	}
}

func TestCreateCipher_InvalidSession(t *testing.T) {
	c := New("http://localhost:8000")

	// Nil session
	_, err := c.CreateCipher(nil, Cipher{Name: "test", Type: 1})
	if err == nil {
		t.Error("expected error for nil session, got nil")
	}

	// Empty access token
	_, err = c.CreateCipher(&Session{AccessToken: ""}, Cipher{Name: "test", Type: 1})
	if err == nil {
		t.Error("expected error for empty access token, got nil")
	}
}

func TestCreateCipher_ServerErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "Bad Request 400",
			statusCode: http.StatusBadRequest,
			body:       `{"message":"Model state is invalid"}`,
		},
		{
			name:       "Internal Server Error 500",
			statusCode: http.StatusInternalServerError,
			body:       `{"message":"Internal error"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := New(srv.URL)
			sess := &Session{AccessToken: "valid-token"}
			_, err := c.CreateCipher(sess, Cipher{Name: "test", Type: 1})
			if err == nil {
				t.Fatalf("expected error for status %d, got nil", tt.statusCode)
			}
		})
	}
}

func TestCreateCipherBodyOmitsEmptyFields(t *testing.T) {
	// Regression: WardenSSH must not POST zero-valued "id"/"notes"/"deletedDate"
	// keys. VaultWarden stores an empty "notes" string, and the BitWarden web
	// vault parses every cipher's notes as an EncString — "" (no dot) crashes
	// it with EncString(InvalidTypeSymm { enc_type: 0, parts: 1 }).
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"c1","name":"2.enc_name==","type":1}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "token"}
	item := Cipher{
		Name: "2.enc_name==",
		Type: 1,
		Login: &Login{
			Username: "2.enc_user==",
			Password: "2.enc_pass==",
		},
		Fields: []CustomField{
			{Name: "2.host==", Value: "2.val==", Type: 0},
		},
	}
	if _, err := c.CreateCipher(sess, item); err != nil {
		t.Fatalf("CreateCipher failed: %v", err)
	}

	for _, key := range []string{`"id"`, `"notes"`, `"deletedDate"`} {
		if strings.Contains(rawBody, key) {
			t.Errorf("request body must not contain %s (empty field leaked into vault): %s", key, rawBody)
		}
	}
}

func TestDeleteCipher_Success(t *testing.T) {
	var capturedMethod, capturedPath, capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL)
	sess := &Session{AccessToken: "delete-token-777"}
	err := c.DeleteCipher(sess, "cipher-id-xyz")
	if err != nil {
		t.Fatalf("DeleteCipher failed: %v", err)
	}

	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedPath != "/api/ciphers/cipher-id-xyz" {
		t.Errorf("path = %q, want /api/ciphers/cipher-id-xyz", capturedPath)
	}
	if capturedAuth != "Bearer delete-token-777" {
		t.Errorf("auth = %q, want 'Bearer delete-token-777'", capturedAuth)
	}
}

func TestDeleteCipher_Error(t *testing.T) {
	c := New("http://localhost:8000")
	if err := c.DeleteCipher(nil, "id"); err == nil {
		t.Error("expected error for nil session")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c2 := New(srv.URL)
	sess := &Session{AccessToken: "valid"}
	if err := c2.DeleteCipher(sess, "missing-id"); err == nil {
		t.Error("expected error for 404 response")
	}
}
