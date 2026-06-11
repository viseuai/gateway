// Package kcadmin is a minimal Keycloak Admin API client for the gateway's
// admin surface: list realm users with roles, grant realm roles. Uses a
// confidential service-account client holding view-users + manage-users.
package kcadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Member is a realm user as the admin surface shows it.
type Member struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
}

// Client talks to one realm's admin API.
type Client struct {
	base, realm, clientID, secret string
	http                          *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

func New(base, realm, clientID, secret string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"), realm: realm,
		clientID: clientID, secret: secret,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// rolesShown on the admin surface; other realm roles are noise there.
var rolesShown = []string{"member", "volunteer-operator", "direcao", "technical-committee", "auditor"}

// Members lists realm users with the governance roles they hold.
func (c *Client) Members(ctx context.Context) ([]Member, error) {
	var users []Member
	if err := c.get(ctx, "/users?max=500", &users); err != nil {
		return nil, err
	}

	holders := map[string]map[string]bool{} // role → user ids
	for _, role := range rolesShown {
		var us []struct {
			ID string `json:"id"`
		}
		if err := c.get(ctx, "/roles/"+role+"/users", &us); err != nil {
			continue // role may not exist; not fatal
		}
		set := map[string]bool{}
		for _, u := range us {
			set[u.ID] = true
		}
		holders[role] = set
	}

	for i := range users {
		for _, role := range rolesShown {
			if holders[role][users[i].ID] {
				users[i].Roles = append(users[i].Roles, role)
			}
		}
	}
	return users, nil
}

// Grant assigns a realm role to a user.
func (c *Client) Grant(ctx context.Context, userID, role string) error {
	var rep struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.get(ctx, "/roles/"+role, &rep); err != nil {
		return fmt.Errorf("resolving role %s: %w", role, err)
	}
	body, _ := json.Marshal([]any{rep})
	return c.do(ctx, http.MethodPost, "/users/"+userID+"/role-mappings/realm", body, nil)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, out any) error {
	tok, err := c.adminToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method,
		c.base+"/admin/realms/"+c.realm+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("keycloak admin %s %s: status %d", method, path, res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (c *Client) adminToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.secret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/realms/"+c.realm+"/protocol/openid-connect/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("keycloak admin token: status %d", res.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil {
		return "", err
	}
	c.token = tok.AccessToken
	c.expires = time.Now().Add(time.Duration(tok.ExpiresIn-10) * time.Second)
	return c.token, nil
}
