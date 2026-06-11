// Package meshkey mints headscale pre-auth keys so node onboarding is
// self-service for the direção. Keys are single-use and force-tagged
// tag:node: whatever joins with them gets the volunteer ACL posture.
package meshkey

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base, apiKey, user string
	http               *http.Client
}

func New(base, apiKey, user string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"), apiKey: apiKey, user: user,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// Mint creates a single-use tag:node pre-auth key valid for expiry.
func (c *Client) Mint(ctx context.Context, expiry time.Duration) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"user":       c.user,
		"reusable":   false,
		"ephemeral":  false,
		"expiration": time.Now().Add(expiry).UTC().Format(time.RFC3339),
		"aclTags":    []string{"tag:node"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/v1/preauthkey", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("headscale preauthkey: status %d", res.StatusCode)
	}
	var out struct {
		PreAuthKey struct {
			Key string `json:"key"`
		} `json:"preAuthKey"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.PreAuthKey.Key == "" {
		return "", fmt.Errorf("headscale returned an empty key")
	}
	return out.PreAuthKey.Key, nil
}
