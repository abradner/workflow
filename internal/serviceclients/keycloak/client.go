// Package keycloak wraps the Keycloak Admin REST API over plain net/http -
// there's no official Go SDK worth pulling in for the handful of endpoints
// this tool needs.
package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a small stateful Keycloak Admin API client: Authenticate once,
// then call the admin methods, which reuse the resulting bearer token.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// New builds a Client for the given Keycloak base URL, e.g.
// "https://pmn-keycloak.wtf.dev4.example.com".
func New(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// Ready reports whether the realm's well-known OIDC config is reachable.
// Any failure (timeout, connection refused, non-2xx) reports false rather
// than an error - this is a polling health check, not a normal request.
func (c *Client) Ready(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/realms/master/.well-known/openid-configuration", nil)
	if err != nil {
		return false
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode/100 == 2
}

// Authenticate performs the OAuth2 password grant against the admin-cli
// client and stores the resulting access token for subsequent requests.
func (c *Client) Authenticate(ctx context.Context, username, password string) error {
	form := url.Values{
		"client_id":  {"admin-cli"},
		"username":   {username},
		"password":   {password},
		"grant_type": {"password"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/realms/master/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	body, err := c.executeBody(req)
	if err != nil {
		return err
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("parsing Keycloak token response: %w", err)
	}
	c.token = parsed.AccessToken
	return nil
}

// CreateRealm creates a realm. A 409 (already exists) is treated as success.
func (c *Client) CreateRealm(ctx context.Context, realmName string) error {
	_, err := c.post(ctx, "/admin/realms", map[string]any{"realm": realmName, "enabled": true})
	return err
}

// ImportClient registers a client application in realmName.
func (c *Client) ImportClient(ctx context.Context, realmName string, clientPayload map[string]any) error {
	_, err := c.post(ctx, fmt.Sprintf("/admin/realms/%s/clients", realmName), clientPayload)
	return err
}

// CreateGroup creates a group in realmName.
func (c *Client) CreateGroup(ctx context.Context, realmName, groupName string) error {
	_, err := c.post(ctx, fmt.Sprintf("/admin/realms/%s/groups", realmName), map[string]any{"name": groupName})
	return err
}

// CreateUser creates a user and returns its ID parsed from the Location
// response header. Returns "" (no error) if the server didn't send one -
// e.g. a 409 because the user already exists, in which case the caller
// should look the user up via GetUsers instead.
func (c *Client) CreateUser(ctx context.Context, realmName string, userPayload map[string]any) (string, error) {
	req, err := c.authedJSONRequest(ctx, http.MethodPost, fmt.Sprintf("/admin/realms/%s/users", realmName), userPayload)
	if err != nil {
		return "", err
	}

	resp, err := c.send(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		if loc := resp.Header.Get("Location"); loc != "" {
			parts := strings.Split(loc, "/")
			return parts[len(parts)-1], nil
		}
	}
	return "", nil
}

// GetUsers looks up users in realmName, optionally filtered by username.
func (c *Client) GetUsers(ctx context.Context, realmName, username string) ([]map[string]any, error) {
	path := fmt.Sprintf("/admin/realms/%s/users", realmName)
	if username != "" {
		path += "?username=" + url.QueryEscape(username)
	}
	return c.getList(ctx, path)
}

// GetGroups looks up groups in realmName, optionally filtered by search term.
func (c *Client) GetGroups(ctx context.Context, realmName, search string) ([]map[string]any, error) {
	path := fmt.Sprintf("/admin/realms/%s/groups", realmName)
	if search != "" {
		path += "?search=" + url.QueryEscape(search)
	}
	return c.getList(ctx, path)
}

// AddUserToGroup assigns userID to groupID within realmName.
func (c *Client) AddUserToGroup(ctx context.Context, realmName, userID, groupID string) error {
	return c.put(ctx, fmt.Sprintf("/admin/realms/%s/users/%s/groups/%s", realmName, userID, groupID), nil)
}

// FetchSAMLDescriptor returns the realm's raw SAML SP descriptor XML.
func (c *Client) FetchSAMLDescriptor(ctx context.Context, realmName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/realms/"+realmName+"/protocol/saml/descriptor", nil)
	if err != nil {
		return "", err
	}
	body, err := c.executeBody(req)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// FetchRealmPublicKey returns the realm's public key.
func (c *Client) FetchRealmPublicKey(ctx context.Context, realmName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/realms/"+realmName, nil)
	if err != nil {
		return "", err
	}
	body, err := c.executeBody(req)
	if err != nil {
		return "", err
	}

	var parsed struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parsing Keycloak realm response: %w", err)
	}
	return parsed.PublicKey, nil
}

func (c *Client) getList(ctx context.Context, path string) ([]map[string]any, error) {
	req, err := c.authedJSONRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	body, err := c.executeBody(req)
	if err != nil {
		return nil, err
	}

	var raw []any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing Keycloak response: %w", err)
	}

	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (c *Client) post(ctx context.Context, path string, payload map[string]any) ([]byte, error) {
	req, err := c.authedJSONRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}
	return c.executeBody(req)
}

func (c *Client) put(ctx context.Context, path string, payload map[string]any) error {
	req, err := c.authedJSONRequest(ctx, http.MethodPut, path, payload)
	if err != nil {
		return err
	}
	_, err = c.executeBody(req)
	return err
}

// authedJSONRequest builds a request carrying the bearer token, erroring if
// Authenticate hasn't been called yet.
func (c *Client) authedJSONRequest(ctx context.Context, method, path string, payload map[string]any) (*http.Request, error) {
	if c.token == "" {
		return nil, fmt.Errorf("not authenticated")
	}

	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// send performs req, treating any 2xx or 409 (Conflict - "already exists",
// which the realm/client/group/user setup flow treats as success) as OK.
func (c *Client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusConflict {
		return resp, nil
	}

	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return nil, fmt.Errorf("keycloak API error: %d %s - %s", resp.StatusCode, resp.Status, string(body))
}

func (c *Client) executeBody(req *http.Request) ([]byte, error) {
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
