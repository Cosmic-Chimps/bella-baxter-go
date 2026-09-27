package provider

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	bellabaxter "github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// #992 — a device key configured on the provider must be PRESENTED.
//
// The SDK this module pins only sends X-E2E-Public-Key when EnableE2EE is set, and the provider
// never set it, so `private_key` / BELLA_BAXTER_PRIVATE_KEY was parsed and dropped: under ZKE
// enforcement every read refused with 403. These tests build the client exactly as Configure does
// (resolveString + clientOptions + bellabaxter.New, against the pinned SDK) and drive a read
// against a stand-in for the API that refuses a read presenting no registered key.

const (
	testAPIKey    = "bax-0123456789abcdef0123456789abcdef-00ff"
	sentinelKey   = "DATABASE_URL"
	sentinelValue = "postgres://device-key-only"
)

type platform struct {
	registered *ecdh.PublicKey // nil: no key registered, plaintext reads allowed

	mu        sync.Mutex
	presented []string
}

func (p *platform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v1/projects/p/environments/e/secrets" {
		http.NotFound(w, r)
		return
	}
	header := r.Header.Get("X-E2E-Public-Key")
	p.mu.Lock()
	p.presented = append(p.presented, header)
	p.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"secrets": map[string]string{sentinelKey: sentinelValue}, "version": 1})
	w.Header().Set("Content-Type", "application/json")
	if header == "" {
		if p.registered != nil {
			http.Error(w, `{"title":"zke-device-required"}`, http.StatusForbidden)
			return
		}
		_, _ = w.Write(body)
		return
	}
	client, err := parseSPKI(header)
	if err != nil || (p.registered != nil && !client.Equal(p.registered)) {
		http.Error(w, `{"title":"zke-device-required"}`, http.StatusForbidden)
		return
	}
	envelope, err := eciesEncrypt(body, client)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(envelope)
}

func parseSPKI(b64 string) (*ecdh.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	switch k := pub.(type) {
	case *ecdh.PublicKey:
		return k, nil
	case *ecdsa.PublicKey:
		return k.ECDH()
	}
	return nil, x509.ErrUnsupportedAlgorithm
}

// eciesEncrypt is the server half of the frozen contract (apps/sdk/crypto/EciesAlgorithm.cs).
func eciesEncrypt(plaintext []byte, client *ecdh.PublicKey) (map[string]any, error) {
	server, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := server.ECDH(client)
	if err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, shared, make([]byte, 32), "bella-e2ee-v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	serverSPKI, err := x509.MarshalPKIXPublicKey(server.PublicKey())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"encrypted":       true,
		"algorithm":       "ECDH-P256-HKDF-SHA256-AES256GCM",
		"serverPublicKey": base64.StdEncoding.EncodeToString(serverSPKI),
		"nonce":           base64.StdEncoding.EncodeToString(nonce),
		"tag":             base64.StdEncoding.EncodeToString(sealed[len(sealed)-16:]),
		"ciphertext":      base64.StdEncoding.EncodeToString(sealed[:len(sealed)-16]),
	}, nil
}

func deviceKey(t *testing.T) (*ecdh.PrivateKey, string) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return priv, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// readThroughProvider builds the client the way Configure does and performs one read.
func readThroughProvider(t *testing.T, srvURL string, configured types.String) (*bellabaxter.AllEnvironmentSecretsResponse, error) {
	t.Helper()
	privateKey := resolveString(configured, "BELLA_BAXTER_PRIVATE_KEY")
	client, err := bellabaxter.New(clientOptions(srvURL, testAPIKey, "", privateKey))
	if err != nil {
		t.Fatalf("bellabaxter.New: %v", err)
	}
	return client.GetAllSecrets(context.Background(), "p", "e")
}

func TestConfiguredDeviceKeyIsPresented(t *testing.T) {
	cases := []struct {
		name string
		// fromConfig: the key is set as `private_key`; otherwise only BELLA_BAXTER_PRIVATE_KEY is.
		fromConfig bool
	}{
		{name: "private_key attribute", fromConfig: true},
		{name: "BELLA_BAXTER_PRIVATE_KEY only", fromConfig: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			priv, pemText := deviceKey(t)
			configured := types.StringNull()
			if tc.fromConfig {
				configured = types.StringValue(pemText)
				t.Setenv("BELLA_BAXTER_PRIVATE_KEY", "")
			} else {
				t.Setenv("BELLA_BAXTER_PRIVATE_KEY", pemText)
			}

			p := &platform{registered: priv.PublicKey()}
			srv := httptest.NewServer(p)
			defer srv.Close()

			resp, err := readThroughProvider(t, srv.URL, configured)
			if err != nil {
				t.Fatalf("read under enforcement with a configured device key: %v", err)
			}
			if resp.Secrets[sentinelKey] != sentinelValue {
				t.Fatalf("decrypted %v", resp.Secrets)
			}
		})
	}
}

// Without a key the provider keeps its old behaviour: no header, plaintext read.
func TestNoDeviceKeyPresentsNothing(t *testing.T) {
	t.Setenv("BELLA_BAXTER_PRIVATE_KEY", "")
	p := &platform{}
	srv := httptest.NewServer(p)
	defer srv.Close()

	if _, err := readThroughProvider(t, srv.URL, types.StringNull()); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(p.presented) != 1 || p.presented[0] != "" {
		t.Fatalf("X-E2E-Public-Key presented = %q, want none", p.presented)
	}
	if opts := clientOptions("u", testAPIKey, "", ""); opts.EnableE2EE {
		t.Fatalf("EnableE2EE set with no device key")
	}
}
