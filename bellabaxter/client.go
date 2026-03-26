// Package bellabaxter provides a Go client for the Bella Baxter secret management API.
//
// # Usage
//
//client, err := bellabaxter.New(bellabaxter.Options{
//    BaxterURL: "https://api.bella-baxter.io",
//    ApiKey:    "bax-7e98d73e4023419aacde52c1d360bbd8-a3f9c8d2e1b4a7f6e8c2d4b6f8e1b4a7",
//})
//if err != nil {
//    log.Fatal(err)
//}
//defer client.Close()
//
//resp, err := client.GetAllSecrets(ctx, "my-project", "production")
//if err != nil {
//    log.Fatal(err)
//}
//fmt.Println(resp.Secrets["DATABASE_URL"])
package bellabaxter

import (
"context"
	"encoding/json"
	"errors"
	"io"
"encoding/hex"
"fmt"
"net/http"
"os"
"strings"
"time"

kiotaabstractions "github.com/microsoft/kiota-abstractions-go"
	kiotaser "github.com/microsoft/kiota-abstractions-go/serialization"
kiotaauth "github.com/microsoft/kiota-abstractions-go/authentication"
kiotaform "github.com/microsoft/kiota-serialization-form-go"
kiotajson "github.com/microsoft/kiota-serialization-json-go"
kiotamultipart "github.com/microsoft/kiota-serialization-multipart-go"
kiotatext "github.com/microsoft/kiota-serialization-text-go"
kiotahttp "github.com/microsoft/kiota-http-go"

"github.com/cosmic-chimps/bella-baxter-go/generated"
)

// Options configures the BaxterClient.
type Options struct {
// BaxterURL is the base URL of the Bella Baxter API (e.g. "https://baxter.example.com").
BaxterURL string

// ApiKey is a Bella Baxter API key (starts with "bax-").
// Obtain one from the WebApp under Project → API Keys.
ApiKey string

// Timeout is the per-request HTTP timeout (default: 10s).
Timeout time.Duration

// EnableE2EE enables end-to-end encryption for secrets responses.
// When true, a P-256 keypair is generated and X-E2E-Public-Key is sent with
// every secrets request so the server encrypts the response payload.
// Decryption happens automatically and transparently.
EnableE2EE bool

// Debug logs every HTTP request and response to stderr.
// Can also be enabled by setting the BELLA_DEBUG=1 environment variable.
// Sensitive headers (X-Bella-Key-Id, X-Bella-Signature) are masked automatically.
Debug bool

// AppClient is the name of your application, sent as the X-App-Client header
// for audit logging. Falls back to the BELLA_BAXTER_APP_CLIENT environment variable.
// Example: "my-web-api", "payment-service", "data-pipeline"
AppClient string
}

// Client is a thread-safe Bella Baxter API client backed by the Kiota generated SDK.
//
// Create one client per application and reuse it.
type Client struct {
	kiota      *generated.BellaClient
	baseURL    string
	httpClient *http.Client // kept for write.go raw operations
}

// New creates a new Client and validates the provided Options.
func New(opts Options) (*Client, error) {
if strings.TrimSpace(opts.BaxterURL) == "" {
	opts.BaxterURL = "https://api.bella-baxter.io"
}
if strings.TrimSpace(opts.ApiKey) == "" {
return nil, fmt.Errorf("bellabaxter: ApiKey must not be empty")
}
if !strings.HasPrefix(strings.TrimSpace(opts.ApiKey), "bax-") {
return nil, fmt.Errorf("bellabaxter: ApiKey must start with 'bax-'")
}

timeout := opts.Timeout
if timeout == 0 {
timeout = 10 * time.Second
}

// Build transport chain: (debug) → HMAC → (optional E2EE) → default
var transport http.RoundTripper = http.DefaultTransport
if opts.EnableE2EE {
e2ee, err := newE2EERoundTripper(transport)
if err != nil {
return nil, fmt.Errorf("bellabaxter: E2EE init: %w", err)
}
transport = e2ee
}
transport = &hmacRoundTripper{
base:          transport,
keyID:         parseKeyID(opts.ApiKey),
signingSecret: parseSigningSecret(opts.ApiKey),
appClient:     firstNonEmpty(opts.AppClient, os.Getenv("BELLA_BAXTER_APP_CLIENT")),
}
if opts.Debug || os.Getenv("BELLA_DEBUG") == "1" || os.Getenv("BELLA_DEBUG") == "true" {
transport = &loggingRoundTripper{base: transport}
}

httpClient := &http.Client{
Transport: transport,
Timeout:   timeout,
}

// Register Kiota serialization factories (idempotent — safe to call multiple times)
kiotaabstractions.RegisterDefaultSerializer(func() kiotaser.SerializationWriterFactory {
return kiotajson.NewJsonSerializationWriterFactory()
})
kiotaabstractions.RegisterDefaultSerializer(func() kiotaser.SerializationWriterFactory {
return kiotatext.NewTextSerializationWriterFactory()
})
kiotaabstractions.RegisterDefaultSerializer(func() kiotaser.SerializationWriterFactory {
return kiotaform.NewFormSerializationWriterFactory()
})
kiotaabstractions.RegisterDefaultSerializer(func() kiotaser.SerializationWriterFactory {
return kiotamultipart.NewMultipartSerializationWriterFactory()
})
kiotaabstractions.RegisterDefaultDeserializer(func() kiotaser.ParseNodeFactory {
return kiotajson.NewJsonParseNodeFactory()
})
kiotaabstractions.RegisterDefaultDeserializer(func() kiotaser.ParseNodeFactory {
return kiotatext.NewTextParseNodeFactory()
})
kiotaabstractions.RegisterDefaultDeserializer(func() kiotaser.ParseNodeFactory {
return kiotaform.NewFormParseNodeFactory()
})

adapter, err := kiotahttp.NewNetHttpRequestAdapterWithParseNodeFactoryAndSerializationWriterFactoryAndHttpClient(
&kiotaauth.AnonymousAuthenticationProvider{}, // HMAC transport does auth
nil, // default parse node factory
nil, // default serialization writer factory
httpClient,
)
if err != nil {
return nil, fmt.Errorf("bellabaxter: create adapter: %w", err)
}
adapter.SetBaseUrl(strings.TrimRight(opts.BaxterURL, "/"))

return &Client{kiota: generated.NewBellaClient(adapter), baseURL: strings.TrimRight(opts.BaxterURL, "/"), httpClient: httpClient}, nil
}

// Close releases any resources held by the client.
func (c *Client) Close() {}

// ── Public API ─────────────────────────────────────────────────────────────────

// GetAllSecrets fetches all secrets for an environment aggregated across all assigned providers.
//
// Results are served from Baxter's Redis cache. When EnableE2EE is true,
// the response is encrypted by the server and decrypted transparently.
func (c *Client) GetAllSecrets(ctx context.Context, projectRef, envSlug string) (*AllEnvironmentSecretsResponse, error) {
	resp, err := c.kiota.Api().V1().Projects().ById(projectRef).
		Environments().ByEnvSlug(envSlug).
		Secrets().Get(ctx, nil)
if err != nil {
return nil, fmt.Errorf("bellabaxter: GetAllSecrets: %w", err)
}

secrets := make(map[string]string)
if resp.GetSecrets() != nil {
for k, v := range resp.GetSecrets().GetAdditionalData() {
// Go Kiota stores JSON string values as *string in AdditionalData (not bare string).
switch typedV := v.(type) {
case string:
secrets[k] = typedV
case *string:
if typedV != nil {
secrets[k] = *typedV
}
}
}
}

version := int64(0)
if resp.GetVersion() != nil {
version = *resp.GetVersion()
}

return &AllEnvironmentSecretsResponse{
Secrets: secrets,
Version: version,
}, nil
}

// GetKeyContext calls GET /api/v1/keys/me and returns the project + environment
// context that the API key is scoped to. Use this to discover projectSlug /
// environmentSlug without requiring the caller to provide them explicitly.
func (c *Client) GetKeyContext(ctx context.Context) (*KeyContextResponse, error) {
	var resp KeyContextResponse
	if err := c.doGet(ctx, "/api/v1/keys/me", &resp); err != nil {
		return nil, fmt.Errorf("bellabaxter: GetKeyContext: %w", err)
	}
	return &resp, nil
}

// GetSecretsVersion returns only the version counter for an environment.
// Use this before GetAllSecrets to avoid transferring all secrets when nothing changed.
func (c *Client) GetSecretsVersion(ctx context.Context, projectRef, envSlug string) (*EnvironmentSecretsVersionResponse, error) {
	resp, err := c.kiota.Api().V1().Projects().ById(projectRef).
		Environments().ByEnvSlug(envSlug).
		Secrets().Version().Get(ctx, nil)
if err != nil {
return nil, fmt.Errorf("bellabaxter: GetSecretsVersion: %w", err)
}

version := int64(0)
if resp.GetVersion() != nil {
version = *resp.GetVersion()
}

return &EnvironmentSecretsVersionResponse{Version: version}, nil
}

// ── Key parsing helpers ────────────────────────────────────────────────────────

// parseKeyID extracts the 32-hex keyId from "bax-{keyId}-{secret}".
func parseKeyID(apiKey string) string {
parts := strings.SplitN(apiKey, "-", 3)
if len(parts) == 3 {
return parts[1]
}
return ""
}

// parseSigningSecret extracts the raw signing secret bytes from "bax-{keyId}-{secret}".
func parseSigningSecret(apiKey string) []byte {
parts := strings.SplitN(apiKey, "-", 3)
if len(parts) == 3 {
b, _ := hex.DecodeString(parts[2])
return b
}
return nil
}

// ── Errors ─────────────────────────────────────────────────────────────────────

// AuthError is returned when the server rejects credentials.
type AuthError struct{ Message string }

func (e *AuthError) Error() string { return "bellabaxter: auth error: " + e.Message }

// NotFoundError is returned when the requested resource does not exist
// or the caller lacks permission (Bella returns 404 to avoid leaking existence).
type NotFoundError struct{ Path string }

func (e *NotFoundError) Error() string { return "bellabaxter: not found: " + e.Path }

// IsNotFoundError reports whether err represents a 404 response from Bella Baxter.
// It handles both the manual-HTTP NotFoundError (from write.go / doGet) and the
// Kiota ApiError (from Kiota-generated fluent API calls like GetAllSecrets).
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var nfe *NotFoundError
	if errors.As(err, &nfe) {
		return true
	}
	// Kiota wraps HTTP errors as *abstractions.ApiError with ResponseStatusCode set.
	var apiErr *kiotaabstractions.ApiError
	if errors.As(err, &apiErr) {
		return apiErr.ResponseStatusCode == 404
	}
	return false
}

// ── Backward-compat helpers used by write.go ────────────────────────────────

// sign is a no-op stub: httpClient.Transport (hmacRoundTripper) signs every
// request transparently, so write.go's explicit sign() call produces no additional headers.
func (c *Client) sign(method, path string, body []byte) map[string]string {
	_, _, _ = method, path, body
	return nil
}

// doGet performs a raw GET for write.go operations that haven't been migrated to Kiota yet.
func (c *Client) doGet(ctx context.Context, path string, out any) error {
req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
if err != nil {
return fmt.Errorf("bellabaxter: build request: %w", err)
}
req.Header.Set("Accept", "application/json")

resp, err := c.httpClient.Do(req)
if err != nil {
return fmt.Errorf("bellabaxter: %s: %w", path, err)
}
defer resp.Body.Close()

switch resp.StatusCode {
case http.StatusOK:
case http.StatusUnauthorized:
return &AuthError{Message: "unauthorized"}
case http.StatusNotFound:
return &NotFoundError{Path: path}
default:
b, _ := io.ReadAll(resp.Body)
return fmt.Errorf("bellabaxter: %s returned HTTP %d: %s", path, resp.StatusCode, b)
}

return json.NewDecoder(resp.Body).Decode(out)
}

// firstNonEmpty returns the first non-empty string from the provided values.
func firstNonEmpty(vals ...string) string {
for _, v := range vals {
if v != "" {
return v
}
}
return ""
}
