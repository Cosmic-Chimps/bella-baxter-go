package bellabaxter

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/hkdf"
)

// #992 — a device key that is supplied must be PRESENTED.
//
// New used to parse BELLA_BAXTER_PRIVATE_KEY (or Options.PrivateKeyPEM) and then drop it on the
// floor unless the caller had also set EnableE2EE: true: no X-E2E-Public-Key, no decryption, no
// warning. Under ZKE enforcement (specs 037/039) every read then came back 403 while the device
// key sat loaded and unused — the same silent shape as #989.
//
// These tests run the real client against an httptest server that behaves like the platform: it
// refuses a secrets read that presents no key (or the wrong one) when enforcing, and otherwise
// answers with the same ECIES envelope the API produces (EciesAlgorithm.Encrypt). So what they
// assert is what the application would observe, not the shape of the code.

const (
	testAPIKey      = "bax-0123456789abcdef0123456789abcdef-00ff"
	sentinelKey     = "DATABASE_URL"
	sentinelValue   = "postgres://device-key-only"
	secretsReadPath = "/api/v1/projects/p/environments/e/secrets"
)

// platform is a stand-in for the Bella API's secrets read.
type platform struct {
	// registered is the device key enforcement admits; nil admits any presented key.
	registered *ecdh.PublicKey
	// enforce refuses a read without an admitted X-E2E-Public-Key, as ZKE enforcement does.
	enforce bool

	mu        sync.Mutex
	presented []string // the X-E2E-Public-Key of every secrets read, "" when absent
}

func (p *platform) presentedKeys() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.presented...)
}

func (p *platform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != secretsReadPath {
		http.NotFound(w, r)
		return
	}
	header := r.Header.Get("X-E2E-Public-Key")
	p.mu.Lock()
	p.presented = append(p.presented, header)
	p.mu.Unlock()

	body, _ := json.Marshal(map[string]any{
		"secrets": map[string]string{sentinelKey: sentinelValue},
		"version": 7,
	})

	if header == "" {
		if p.enforce {
			http.Error(w, `{"title":"zke-device-required"}`, http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
		return
	}

	client, err := parseSPKI(header)
	if err != nil {
		http.Error(w, "bad X-E2E-Public-Key", http.StatusBadRequest)
		return
	}
	if p.enforce && p.registered != nil && !client.Equal(p.registered) {
		http.Error(w, `{"title":"zke-device-required"}`, http.StatusForbidden)
		return
	}

	envelope, err := eciesEncrypt(body, client)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
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
	default:
		return nil, fmt.Errorf("unexpected public key type %T", pub)
	}
}

// eciesEncrypt is the server half of the frozen wire contract (apps/sdk/crypto/EciesAlgorithm.cs):
// ephemeral P-256 ECDH, HKDF-SHA-256 (32 zero-byte salt, info "bella-e2ee-v1"), AES-256-GCM with
// a 12-byte nonce and a separate 16-byte tag, server key as base64 SPKI.
func eciesEncrypt(plaintext []byte, client *ecdh.PublicKey) (map[string]any, error) {
	server, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := server.ECDH(client)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, make([]byte, 32), []byte("bella-e2ee-v1")), key); err != nil {
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
	ciphertext, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	serverSPKI, err := x509.MarshalPKIXPublicKey(server.PublicKey())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"encrypted":       true,
		"algorithm":       "ECDH-P256-HKDF-SHA256-AES256GCM",
		"serverPublicKey": base64.StdEncoding.EncodeToString(serverSPKI),
		"nonce":           base64.StdEncoding.EncodeToString(nonce),
		"tag":             base64.StdEncoding.EncodeToString(tag),
		"ciphertext":      base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

// deviceKey returns a fresh P-256 device key as the PKCS#8 PEM `bella sdk run` injects, and as
// the SPKI base64 the platform would have registered for it.
func deviceKey(t *testing.T) (priv *ecdh.PrivateKey, pemText string, pkcs8 []byte) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err = x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pemText = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))
	return priv, pemText, pkcs8
}

func spkiB64(t *testing.T, pub *ecdh.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

// captureLog routes the standard logger (the SDK's only logging hook) into a buffer.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var sink bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&sink)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &sink
}

// An application run under `bella sdk run` sets nothing but what the CLI injects. The key is in
// the environment and no flag is set: the client must present that key, and decrypt with it.
func TestEnvDeviceKeyIsPresentedAndDecryptsWithoutAnyFlag(t *testing.T) {
	priv, pemText, _ := deviceKey(t)
	t.Setenv("BELLA_BAXTER_PRIVATE_KEY", pemText)

	p := &platform{registered: priv.PublicKey(), enforce: true}
	srv := httptest.NewServer(p)
	defer srv.Close()

	client, err := New(Options{BaxterURL: srv.URL, ApiKey: testAPIKey})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := client.GetAllSecrets(context.Background(), "p", "e")
	if err != nil {
		t.Fatalf("GetAllSecrets under enforcement with the device key in the environment: %v", err)
	}
	if got := resp.Secrets[sentinelKey]; got != sentinelValue {
		t.Fatalf("decrypted %s=%q, want %q", sentinelKey, got, sentinelValue)
	}
	if got := p.presentedKeys(); len(got) != 1 || got[0] != spkiB64(t, priv.PublicKey()) {
		t.Fatalf("X-E2E-Public-Key presented = %q, want the device key's SPKI", got)
	}
}

// The same key must load whichever form it arrives in, and whichever way it is supplied.
func TestDeviceKeyIsPresentedInEveryFormAndSource(t *testing.T) {
	cases := []struct {
		name     string
		env      func(pemText string, der []byte) string
		explicit func(pemText string, der []byte) string
	}{
		{name: "env PEM with CRLF line endings",
			env: func(p string, _ []byte) string { return strings.ReplaceAll(p, "\n", "\r\n") }},
		{name: "env bare base64 DER",
			env: func(_ string, d []byte) string { return base64.StdEncoding.EncodeToString(d) }},
		{name: "explicit PEM",
			explicit: func(p string, _ []byte) string { return p }},
		{name: "explicit wins over env",
			env:      func(string, []byte) string { return "not a key, and not consulted" },
			explicit: func(p string, _ []byte) string { return p }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			priv, pemText, der := deviceKey(t)
			env, explicit := "", ""
			if tc.env != nil {
				env = tc.env(pemText, der)
			}
			if tc.explicit != nil {
				explicit = tc.explicit(pemText, der)
			}
			t.Setenv(privateKeyEnvVar, env)

			p := &platform{registered: priv.PublicKey(), enforce: true}
			srv := httptest.NewServer(p)
			defer srv.Close()

			client, err := New(Options{BaxterURL: srv.URL, ApiKey: testAPIKey, PrivateKeyPEM: explicit})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			resp, err := client.GetAllSecrets(context.Background(), "p", "e")
			if err != nil {
				t.Fatalf("GetAllSecrets: %v", err)
			}
			if resp.Secrets[sentinelKey] != sentinelValue {
				t.Fatalf("decrypted %v", resp.Secrets)
			}
		})
	}
}

// DisableE2EE is the explicit opt-out. The key is then not presented, and because that is
// almost certainly not what someone who supplied a device key meant, New says so exactly once,
// on the standard logger, and never on stdout.
func TestDisableE2EEDoesNotPresentTheKeyAndWarnsOnce(t *testing.T) {
	_, pemText, _ := deviceKey(t)
	t.Setenv(privateKeyEnvVar, pemText)
	logged := captureLog(t)

	p := &platform{} // not enforcing, so the plaintext read succeeds and we can see what was sent
	srv := httptest.NewServer(p)
	defer srv.Close()

	var client *Client
	stdout := captureStdout(t, func() {
		var err error
		client, err = New(Options{BaxterURL: srv.URL, ApiKey: testAPIKey, DisableE2EE: true})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
	})
	for i := 0; i < 2; i++ {
		resp, err := client.GetAllSecrets(context.Background(), "p", "e")
		if err != nil {
			t.Fatalf("GetAllSecrets: %v", err)
		}
		if resp.Secrets[sentinelKey] != sentinelValue {
			t.Fatalf("read %v", resp.Secrets)
		}
	}

	if got := p.presentedKeys(); len(got) != 2 || got[0] != "" || got[1] != "" {
		t.Fatalf("X-E2E-Public-Key presented = %q, want none under DisableE2EE", got)
	}
	out := logged.String()
	if n := strings.Count(out, "[BELLA] warning:"); n != 1 {
		t.Fatalf("want exactly one warning across construction and two reads, got %d:\n%s", n, out)
	}
	for _, want := range []string{privateKeyEnvVar, "DisableE2EE", "NOT presented"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning does not mention %q:\n%s", want, out)
		}
	}
	firstBodyLine := strings.Split(pemText, "\n")[1]
	if strings.Contains(out, "PRIVATE KEY-----") || strings.Contains(out, firstBodyLine) {
		t.Errorf("warning leaked key material:\n%s", out)
	}
	if stdout != "" {
		t.Errorf("New wrote to stdout: %q", stdout)
	}
}

// DisableE2EE with no key is simply "no E2EE": nothing to warn about.
func TestDisableE2EEWithoutAKeyIsSilent(t *testing.T) {
	t.Setenv(privateKeyEnvVar, "")
	logged := captureLog(t)
	if _, err := New(Options{BaxterURL: "http://127.0.0.1:1", ApiKey: testAPIKey, DisableE2EE: true}); err != nil {
		t.Fatalf("New: %v", err)
	}
	if logged.Len() != 0 {
		t.Fatalf("unexpected log output: %s", logged.String())
	}
}

func TestEnableAndDisableTogetherIsAnError(t *testing.T) {
	t.Setenv(privateKeyEnvVar, "")
	_, err := New(Options{ApiKey: testAPIKey, EnableE2EE: true, DisableE2EE: true})
	if err == nil || !strings.Contains(err.Error(), "EnableE2EE and DisableE2EE") {
		t.Fatalf("want a contradiction error, got %v", err)
	}
}

// A key that is present but unreadable stops New, naming where it came from. It must never be
// swapped for an ephemeral key, which would present a key nobody registered.
func TestUnreadableDeviceKeyFailsLoudly(t *testing.T) {
	p384, err := ecdh.P384().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p384DER, err := x509.MarshalPKCS8PrivateKey(p384)
	if err != nil {
		t.Fatal(err)
	}
	p384PEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: p384DER}))

	cases := []struct {
		name, env, explicit, wantSource string
	}{
		{name: "garbage in env", env: "definitely-not-a-key!", wantSource: privateKeyEnvVar},
		{name: "valid base64, not a key", env: base64.StdEncoding.EncodeToString([]byte("hello")), wantSource: privateKeyEnvVar},
		{name: "PEM armour, broken body", env: "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n", wantSource: privateKeyEnvVar},
		{name: "wrong curve (P-384)", env: p384PEM, wantSource: privateKeyEnvVar},
		{name: "garbage explicit", explicit: "nope", wantSource: "Options.PrivateKeyPEM"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(privateKeyEnvVar, tc.env)
			for _, disable := range []bool{false, true} {
				client, err := New(Options{ApiKey: testAPIKey, PrivateKeyPEM: tc.explicit, DisableE2EE: disable})
				if err == nil {
					t.Fatalf("DisableE2EE=%v: New succeeded with an unreadable key (client %v)", disable, client)
				}
				if !strings.Contains(err.Error(), tc.wantSource) {
					t.Fatalf("DisableE2EE=%v: error does not name %s: %v", disable, tc.wantSource, err)
				}
			}
		})
	}
}

// No key and EnableE2EE: true is today's ephemeral mode, unchanged: a fresh key per client,
// presented and used to decrypt.
func TestEnableE2EEWithoutAKeyUsesAnEphemeralKey(t *testing.T) {
	t.Setenv(privateKeyEnvVar, "")

	p := &platform{} // admits any presented key
	srv := httptest.NewServer(p)
	defer srv.Close()

	for i := 0; i < 2; i++ {
		client, err := New(Options{BaxterURL: srv.URL, ApiKey: testAPIKey, EnableE2EE: true})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		resp, err := client.GetAllSecrets(context.Background(), "p", "e")
		if err != nil {
			t.Fatalf("GetAllSecrets: %v", err)
		}
		if resp.Secrets[sentinelKey] != sentinelValue {
			t.Fatalf("decrypted %v", resp.Secrets)
		}
	}
	got := p.presentedKeys()
	if len(got) != 2 || got[0] == "" || got[1] == "" || got[0] == got[1] {
		t.Fatalf("want two different ephemeral keys presented, got %q", got)
	}
}

// No key and no flag stays exactly as it was: no header, plaintext read. A blank variable is
// "no key", not an unreadable one.
func TestNoKeyNoFlagPresentsNothing(t *testing.T) {
	t.Setenv(privateKeyEnvVar, "   ")

	p := &platform{}
	srv := httptest.NewServer(p)
	defer srv.Close()

	client, err := New(Options{BaxterURL: srv.URL, ApiKey: testAPIKey})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.GetAllSecrets(context.Background(), "p", "e"); err != nil {
		t.Fatalf("GetAllSecrets: %v", err)
	}
	if got := p.presentedKeys(); len(got) != 1 || got[0] != "" {
		t.Fatalf("X-E2E-Public-Key presented = %q, want none", got)
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = previous
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}
