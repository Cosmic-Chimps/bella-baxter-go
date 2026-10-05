package bellabaxter

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #1050 (b) — once this client has presented its E2EE public key, a secrets read that does not come
// back as an envelope decrypting with that key is REFUSED (apps/sdk/SDK_CONTRACT.md, "Rule: a presented
// key requires an envelope"). Before this, a plaintext answer was passed through as if it had been
// end-to-end encrypted.
//
// The stub is a real HTTP server that misbehaves on purpose; the client is the real New + GetAllSecrets.

// misbehavingServer answers the secrets read according to mode, always to a correctly presented key.
func misbehavingServer(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("X-E2E-Public-Key")
		if header == "" {
			http.Error(w, "no key presented", http.StatusBadRequest)
			return
		}
		client, err := parseSPKI(header)
		if err != nil {
			http.Error(w, "bad key", http.StatusBadRequest)
			return
		}
		body, _ := json.Marshal(map[string]any{
			"environmentSlug": "e",
			"environmentName": "e",
			"secrets":         map[string]string{sentinelKey: sentinelValue},
			"version":         3,
			"lastModified":    "2026-10-04T00:00:00Z",
		})
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "valid":
			env, _ := eciesEncrypt(body, client)
			_ = json.NewEncoder(w).Encode(env)
		case "plaintext":
			_, _ = w.Write(body)
		case "dotenv":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(sentinelKey + "=" + sentinelValue + "\n"))
		case "tampered":
			env, _ := eciesEncrypt(body, client)
			ct, _ := base64.StdEncoding.DecodeString(env["ciphertext"].(string))
			ct[0] ^= 0x01
			env["ciphertext"] = base64.StdEncoding.EncodeToString(ct)
			_ = json.NewEncoder(w).Encode(env)
		case "wrong-key":
			other, _ := ecdh.P256().GenerateKey(rand.Reader)
			env, _ := eciesEncrypt(body, other.PublicKey())
			_ = json.NewEncoder(w).Encode(env)
		case "forbidden":
			http.Error(w, `{"title":"zke-device-required"}`, http.StatusForbidden)
		default:
			t.Errorf("unknown mode %q", mode)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func e2eeClient(t *testing.T, url string) *Client {
	t.Helper()
	t.Setenv("BELLA_BAXTER_PRIVATE_KEY", "")
	c, err := New(Options{BaxterURL: url, ApiKey: testAPIKey, EnableE2EE: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestE2EEValidEnvelopeIsDecrypted(t *testing.T) {
	c := e2eeClient(t, misbehavingServer(t, "valid").URL)
	resp, err := c.GetAllSecrets(context.Background(), "p", "e")
	if err != nil {
		t.Fatalf("GetAllSecrets: %v", err)
	}
	if resp.Secrets[sentinelKey] != sentinelValue {
		t.Fatalf("secrets = %v, want %s=%s", resp.Secrets, sentinelKey, sentinelValue)
	}
}

func TestE2EEResponseIsRefused(t *testing.T) {
	cases := []struct{ mode, code string }{
		{"plaintext", E2EEPlaintextResponse},
		{"dotenv", E2EEPlaintextResponse},
		{"tampered", E2EEDecryptionFailed},
		{"wrong-key", E2EEDecryptionFailed},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			c := e2eeClient(t, misbehavingServer(t, tc.mode).URL)
			resp, err := c.GetAllSecrets(context.Background(), "p", "e")
			if err == nil {
				t.Fatalf("accepted the %s answer after presenting the key: %v", tc.mode, resp.Secrets)
			}
			if resp != nil {
				t.Fatalf("returned a response alongside the error: %v", resp.Secrets)
			}
			var e2ee *E2EEResponseError
			if !errors.As(err, &e2ee) {
				t.Fatalf("error is not an *E2EEResponseError: %T %v", err, err)
			}
			if e2ee.Code != tc.code {
				t.Fatalf("code = %q, want %q", e2ee.Code, tc.code)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.code) {
				t.Fatalf("the code is not visible to the caller: %q", msg)
			}
			if strings.Contains(msg, sentinelValue) {
				t.Fatalf("the message carries the secret value: %q", msg)
			}
		})
	}
}

func TestE2EEDecryptionFailureKeepsItsCause(t *testing.T) {
	c := e2eeClient(t, misbehavingServer(t, "tampered").URL)
	_, err := c.GetAllSecrets(context.Background(), "p", "e")
	var e2ee *E2EEResponseError
	if !errors.As(err, &e2ee) || errors.Unwrap(e2ee) == nil {
		t.Fatalf("want an E2EEResponseError wrapping the GCM failure, got %v", err)
	}
}

// A non-2xx answer is the API's own error, not an E2EE refusal.
func TestE2EENon2xxIsNotAnE2EEError(t *testing.T) {
	c := e2eeClient(t, misbehavingServer(t, "forbidden").URL)
	_, err := c.GetAllSecrets(context.Background(), "p", "e")
	if err == nil {
		t.Fatal("a 403 was accepted")
	}
	var e2ee *E2EEResponseError
	if errors.As(err, &e2ee) {
		t.Fatalf("a 403 became an E2EE error: %v", err)
	}
}

// A /secrets call that carries no value is answered in plain JSON even to a presented key: it must
// still pass through, or the rule would break every metadata read and every write.
func TestE2EEValueLessSecretsCallsStillPassPlaintext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":3}`))
	}))
	t.Cleanup(srv.Close)
	c := e2eeClient(t, srv.URL)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/projects/p/environments/e/secrets/version"},
		{http.MethodGet, "/api/v1/projects/p/environments/e/providers/v/secrets/hash"},
		{http.MethodGet, "/api/v1/projects/p/environments/e/providers/v/secrets/K/metadata"},
		{http.MethodGet, "/api/v1/projects/p/environments/e/providers/v/secrets/K/versions"},
		{http.MethodPost, "/api/v1/projects/p/environments/e/providers/v/secrets"},
	} {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader("{}"))
		resp, err := c.httpClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s: status %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestRequiresEnvelope(t *testing.T) {
	yes := []string{
		"/api/v1/projects/p/secrets",
		"/api/v1/projects/p/environments/e/secrets",
		"/api/v1/projects/p/environments/e/secrets/export",
		"/api/v1/projects/p/environments/e/providers/v/secrets",
		"/api/v1/projects/p/environments/e/providers/v/secrets/export",
		"/api/v1/projects/p/environments/e/providers/v/secrets/DATABASE_URL",
		"/api/v1/projects/p/environments/e/providers/v/secrets/DATABASE_URL/versions/12",
		"/gateway/api/v1/projects/p/environments/e/secrets",
	}
	no := []string{
		"/api/v1/projects/p/environments/e/secrets/version",
		"/api/v1/projects/p/environments/e/secrets/manifest",
		"/api/v1/projects/p/environments/e/secrets/certificates",
		"/api/v1/projects/p/environments/e/providers/v/secrets/hash",
		"/api/v1/projects/p/environments/e/providers/v/secrets/import/preview",
		"/api/v1/projects/p/environments/e/providers/v/secrets/K/metadata",
		"/api/v1/projects/p/environments/e/providers/v/secrets/K/versions",
		"/api/v1/projects/p/environments/e/providers/v/secrets/K/versions/latest",
		"/api/v1/projects/p/environments/e/providers/v/secrets/K/rotation-policy",
		"/api/v1/projects/p",
		"/api/v1/tenants/me/zke",
	}
	for _, p := range yes {
		if !requiresEnvelope(http.MethodGet, p) {
			t.Errorf("GET %s should require an envelope", p)
		}
		if requiresEnvelope(http.MethodPost, p) {
			t.Errorf("POST %s should not require an envelope", p)
		}
	}
	for _, p := range no {
		if requiresEnvelope(http.MethodGet, p) {
			t.Errorf("GET %s should not require an envelope", p)
		}
	}
}
