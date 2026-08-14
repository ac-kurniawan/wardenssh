package vaultclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// CreateCipher posts a new Cipher item to VaultWarden / BitWarden via POST /api/ciphers.
// It requires an authenticated Session with a valid AccessToken.
func (c *Client) CreateCipher(sess *Session, item Cipher) (*Cipher, error) {
	if sess == nil || sess.AccessToken == "" {
		return nil, errors.New("vaultclient: session or access token is required")
	}

	body, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("create cipher: marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/api/ciphers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create cipher: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sess.AccessToken)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create cipher: status %d: %s", resp.StatusCode, string(raw))
	}

	var created Cipher
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("create cipher: decode: %w", err)
	}

	return &created, nil
}
