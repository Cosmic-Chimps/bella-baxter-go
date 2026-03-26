package bellabaxter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ── Write-side models ──────────────────────────────────────────────────────────

// EnvironmentResponse is returned by GetEnvironmentByID.
type EnvironmentResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	ProjectID string `json:"projectId"`
}

// SecretOperationResponse is returned by CreateSecret, UpdateSecret, DeleteSecret.
type SecretOperationResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Key     string `json:"key,omitempty"`
}

// ── Write API methods ──────────────────────────────────────────────────────────

// GetEnvironment retrieves an environment by project + env slug.
func (c *Client) GetEnvironment(ctx context.Context, projectSlug, envSlug string) (*EnvironmentResponse, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s",
		url.PathEscape(projectSlug), url.PathEscape(envSlug))
	var resp EnvironmentResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateSecret creates a secret in a specific environment+provider.
func (c *Client) CreateSecret(ctx context.Context, projectSlug, envSlug, providerSlug, key, value, description string) error {
	body := map[string]any{
		"key":   key,
		"value": value,
	}
	if description != "" {
		body["description"] = description
	}
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/providers/%s/secrets",
		url.PathEscape(projectSlug), url.PathEscape(envSlug), url.PathEscape(providerSlug))
	var resp SecretOperationResponse
	return c.doWrite(ctx, http.MethodPost, path, body, &resp)
}

// UpdateSecret updates an existing secret's value and optional description.
func (c *Client) UpdateSecret(ctx context.Context, projectSlug, envSlug, providerSlug, key, value, description string) error {
	body := map[string]any{
		"value": value,
	}
	if description != "" {
		body["description"] = description
	}
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/providers/%s/secrets/%s",
		url.PathEscape(projectSlug), url.PathEscape(envSlug), url.PathEscape(providerSlug), url.PathEscape(key))
	var resp SecretOperationResponse
	return c.doWrite(ctx, http.MethodPut, path, body, &resp)
}

// DeleteSecret removes a secret from a specific environment+provider.
func (c *Client) DeleteSecret(ctx context.Context, projectSlug, envSlug, providerSlug, key string) error {
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/providers/%s/secrets/%s",
		url.PathEscape(projectSlug), url.PathEscape(envSlug), url.PathEscape(providerSlug), url.PathEscape(key))
	return c.doWrite(ctx, http.MethodDelete, path, nil, nil)
}

// ── HTTP write helper ──────────────────────────────────────────────────────────

func (c *Client) doWrite(ctx context.Context, method, path string, body any, out any) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("bellabaxter: marshal body: %w", err)
		}
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("bellabaxter: build request: %w", err)
	}
	for k, v := range c.sign(method, path, bodyBytes) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bellabaxter: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		// success
	case http.StatusUnauthorized:
		return &AuthError{Message: "unauthorized — token may have expired"}
	case http.StatusNotFound:
		return &NotFoundError{Path: path}
	default:
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bellabaxter: %s %s returned HTTP %d: %s", method, path, resp.StatusCode, b)
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("bellabaxter: decode response: %w", err)
		}
	}
	return nil
}

// ── SSH API methods ────────────────────────────────────────────────────────────

// GetSshCaPublicKey returns the SSH CA public key for an environment (auto-selects Vault provider).
func (c *Client) GetSshCaPublicKey(ctx context.Context, projectSlug, envSlug string) (*SshCaPublicKeyResponse, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/ssh/ca-public-key",
		url.PathEscape(projectSlug), url.PathEscape(envSlug))
	var resp SshCaPublicKeyResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListSshRoles lists all SSH roles for an environment.
func (c *Client) ListSshRoles(ctx context.Context, projectSlug, envSlug string) (*SshRolesResponse, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/ssh/roles",
		url.PathEscape(projectSlug), url.PathEscape(envSlug))
	var resp SshRolesResponse
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateSshRole creates an SSH role for an environment.
func (c *Client) CreateSshRole(ctx context.Context, projectSlug, envSlug string, req SshCreateRoleRequest) error {
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/ssh/roles",
		url.PathEscape(projectSlug), url.PathEscape(envSlug))
	return c.doWrite(ctx, http.MethodPost, path, req, nil)
}

// DeleteSshRole deletes an SSH role by name.
func (c *Client) DeleteSshRole(ctx context.Context, projectSlug, envSlug, roleName string) error {
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/ssh/roles/%s",
		url.PathEscape(projectSlug), url.PathEscape(envSlug), url.PathEscape(roleName))
	return c.doWrite(ctx, http.MethodDelete, path, nil, nil)
}

// SignSshKey signs a public key and returns a short-lived SSH certificate.
func (c *Client) SignSshKey(ctx context.Context, projectSlug, envSlug string, req SshSignRequest) (*SshSignedCertResponse, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/environments/%s/ssh/sign",
		url.PathEscape(projectSlug), url.PathEscape(envSlug))
	var resp SshSignedCertResponse
	if err := c.doWrite(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
