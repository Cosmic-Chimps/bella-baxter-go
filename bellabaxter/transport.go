package bellabaxter

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

var (
	hkdfInfo = []byte("bella-e2ee-v1")
	hkdfSalt = make([]byte, 32) // 32 zeros
)

// hmacRoundTripper signs every request with Bella's HMAC-SHA256 scheme and
// injects X-Bella-Client / X-App-Client headers for audit logging.
type hmacRoundTripper struct {
	base          http.RoundTripper
	keyID         string
	signingSecret []byte
	appClient     string // optional; sent as X-App-Client if non-empty
}

func (t *hmacRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())

	// Read body for hashing (and restore it)
	var bodyBytes []byte
	if clone.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(clone.Body)
		if err != nil {
			return nil, fmt.Errorf("bellabaxter hmac: reading body: %w", err)
		}
		clone.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// Build sorted query string
	queryParts := make([]string, 0)
	for k, vs := range clone.URL.Query() {
		for _, v := range vs {
			queryParts = append(queryParts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	sort.Strings(queryParts)
	query := strings.Join(queryParts, "&")

	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	bodyHash := sha256.Sum256(bodyBytes)
	bodyHashHex := hex.EncodeToString(bodyHash[:])

	stringToSign := clone.Method + "\n" +
		clone.URL.Path + "\n" +
		query + "\n" +
		timestamp + "\n" +
		bodyHashHex

	mac := hmac.New(sha256.New, t.signingSecret)
	mac.Write([]byte(stringToSign))
	sig := hex.EncodeToString(mac.Sum(nil))

	clone.Header.Set("User-Agent", "bella-go-sdk/1.0")
	clone.Header.Set("X-Bella-Key-Id", t.keyID)
	clone.Header.Set("X-Bella-Timestamp", timestamp)
	clone.Header.Set("X-Bella-Signature", sig)
	clone.Header.Set("X-Bella-Client", "bella-go-sdk")
	if t.appClient != "" {
		clone.Header.Set("X-App-Client", t.appClient)
	}

	return t.base.RoundTrip(clone)
}

// e2eeRoundTripper adds X-E2E-Public-Key to secrets requests and decrypts responses.
type e2eeRoundTripper struct {
	base         http.RoundTripper
	privKey      *ecdh.PrivateKey
	pubB64       string // base64-encoded SPKI public key, sent as header
	onWrappedDEK func(projectSlug, envSlug, wrappedDEK string, leaseExpires *time.Time) // may be nil
}

// newE2EERoundTripper creates an e2eeRoundTripper.
// If persistentKey is non-nil it is used as the device key (ZKE mode);
// otherwise an ephemeral P-256 keypair is generated per-client (original behaviour).
func newE2EERoundTripper(base http.RoundTripper, persistentKey *ecdh.PrivateKey, onWrappedDEK func(string, string, string, *time.Time)) (*e2eeRoundTripper, error) {
	priv := persistentKey
	if priv == nil {
		var err error
		priv, err = ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("bellabaxter e2ee: key gen: %w", err)
		}
	}
	spki := priv.PublicKey().Bytes() // raw P-256 uncompressed point (65 bytes, used for validation)
	if len(spki) != 65 {
		return nil, fmt.Errorf("bellabaxter e2ee: unexpected public key length %d", len(spki))
	}
	spkiEncoded, err := marshalP256SPKI(priv.PublicKey())
	if err != nil {
		return nil, err
	}
	return &e2eeRoundTripper{
		base:         base,
		privKey:      priv,
		pubB64:       base64.StdEncoding.EncodeToString(spkiEncoded),
		onWrappedDEK: onWrappedDEK,
	}, nil
}

// loadPrivateKeyPEM parses a PKCS#8 PEM private key and returns it as *ecdh.PrivateKey.
// Supports both *ecdsa.PrivateKey (most common PEM format) and raw *ecdh.PrivateKey.
func loadPrivateKeyPEM(pemStr string) (*ecdh.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("bellabaxter e2ee: failed to decode PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("bellabaxter e2ee: parse PKCS8: %w", err)
	}
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		return k.ECDH()
	case *ecdh.PrivateKey:
		return k, nil
	default:
		return nil, fmt.Errorf("bellabaxter e2ee: unsupported key type %T, expected P-256 EC key", key)
	}
}

// extractSlugFromPath extracts projectSlug and envSlug from a secrets API path.
// Expected format: /api/v1/projects/{proj}/environments/{env}/secrets[/...]
func extractSlugFromPath(path string) (projectSlug, envSlug string) {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			projectSlug = parts[i+1]
		}
		if p == "environments" && i+1 < len(parts) {
			envSlug = parts[i+1]
		}
	}
	return
}

// marshalP256SPKI encodes a P-256 public key into SubjectPublicKeyInfo DER bytes.
// Equivalent to ECDiffieHellman.ExportSubjectPublicKeyInfo() in .NET.
func marshalP256SPKI(pub *ecdh.PublicKey) ([]byte, error) {
	// OID for id-ecPublicKey: 1.2.840.10045.2.1
	// OID for P-256 (prime256v1): 1.2.840.10045.3.1.7
	// Hardcoded DER prefix for P-256 SPKI (the OIDs are constant)
	prefix := []byte{
		0x30, 0x59, // SEQUENCE
		0x30, 0x13, // SEQUENCE (algorithm)
		0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, // OID id-ecPublicKey
		0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, // OID prime256v1
		0x03, 0x42, 0x00, // BIT STRING, 65 bytes, no padding bits
	}
	raw := pub.Bytes() // 65-byte uncompressed point
	if len(raw) != 65 {
		return nil, fmt.Errorf("bellabaxter e2ee: unexpected public key length %d", len(raw))
	}
	return append(prefix, raw...), nil
}

func (t *e2eeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	isSecretsReq := strings.Contains(req.URL.Path, "/secrets")

	clone := req.Clone(req.Context())
	if isSecretsReq {
		clone.Header.Set("X-E2E-Public-Key", t.pubB64)
	}

	resp, err := t.base.RoundTrip(clone)
	if err != nil || !isSecretsReq || !isOK(resp) {
		return resp, err
	}

	// Try to decrypt the response body
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("bellabaxter e2ee: read response: %w", err)
	}

	var envelope struct {
		Encrypted       bool   `json:"encrypted"`
		Algorithm       string `json:"algorithm"`
		ServerPublicKey string `json:"serverPublicKey"`
		Nonce           string `json:"nonce"`
		Tag             string `json:"tag"`
		Ciphertext      string `json:"ciphertext"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.Encrypted {
		// Not encrypted — pass through as-is
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
	} else {
		secrets, err := t.decrypt(envelope.ServerPublicKey, envelope.Nonce, envelope.Tag, envelope.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("bellabaxter e2ee: decrypt: %w", err)
		}

		// Plaintext is the full response JSON from the server (AllEnvironmentSecretsResponse,
		// flat secrets map, or any other payload). Pass it directly to Kiota for parsing —
		// this preserves all fields (version, environmentSlug, etc.).
		resp.Body = io.NopCloser(bytes.NewReader(secrets))
		resp.ContentLength = int64(len(secrets))
	}

	// Call OnWrappedDEK if the server returned a wrapped DEK header (ZKE support).
	if onWrappedDEK := t.onWrappedDEK; onWrappedDEK != nil {
		if wrapped := resp.Header.Get("X-Bella-Wrapped-Dek"); wrapped != "" {
			projectSlug, envSlug := extractSlugFromPath(req.URL.Path)
			var leaseExpires *time.Time
			if exp := resp.Header.Get("X-Bella-Lease-Expires"); exp != "" {
				if expTime, err := time.Parse(time.RFC3339, exp); err == nil {
					leaseExpires = &expTime
				}
			}
			onWrappedDEK(projectSlug, envSlug, wrapped, leaseExpires)
		}
	}

	return resp, nil
}

// decrypt performs ECDH key agreement, HKDF-SHA256 key derivation, and AES-256-GCM decryption.
// Returns the raw plaintext bytes (the server's JSON response body before encryption).
func (t *e2eeRoundTripper) decrypt(serverPubB64, nonceB64, tagB64, ciphertextB64 string) ([]byte, error) {
	serverPubBytes, err := base64.StdEncoding.DecodeString(serverPubB64)
	if err != nil {
		return nil, err
	}

	// Parse server's ephemeral P-256 public key from SPKI
	// The raw point is the last 65 bytes of the SPKI
	if len(serverPubBytes) < 65 {
		return nil, fmt.Errorf("server public key too short (%d bytes)", len(serverPubBytes))
	}
	rawServerPub := serverPubBytes[len(serverPubBytes)-65:]
	serverPub, err := ecdh.P256().NewPublicKey(rawServerPub)
	if err != nil {
		return nil, fmt.Errorf("parse server pub key: %w", err)
	}

	sharedSecret, err := t.privKey.ECDH(serverPub)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}

	// HKDF-SHA256 → 32-byte AES key
	hkdfReader := hkdf.New(sha256.New, sharedSecret, hkdfSalt, hkdfInfo)
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(hkdfReader, aesKey); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return nil, err
	}
	tag, err := base64.StdEncoding.DecodeString(tagB64)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithTagSize(block, 16)
	if err != nil {
		return nil, err
	}

	// AES-GCM: ciphertext || tag (tag appended per GCM convention)
	plaintext, err := gcm.Open(nil, nonce, append(ciphertext, tag...), nil)
	if err != nil {
		return nil, fmt.Errorf("aes-gcm decrypt: %w", err)
	}
	return plaintext, nil
}

// isOK returns true for 2xx responses.
func isOK(r *http.Response) bool {
	return r != nil && r.StatusCode >= 200 && r.StatusCode < 300
}

// ── Debug logging transport ────────────────────────────────────────────────────

// loggingRoundTripper logs every HTTP request and response to stderr.
// Enable by setting the BELLA_DEBUG=1 environment variable.
//
// Sensitive headers (Authorization, X-Bella-Key-Id, X-Bella-Signature, Cookie)
// are masked so credentials never appear in logs.
type loggingRoundTripper struct {
	base http.RoundTripper
}

var maskedHeaders = map[string]bool{
	"Authorization":    true,
	"X-Bella-Key-Id":   true,
	"X-Bella-Signature": true,
	"Cookie":           true,
}

func (t *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// ── Request ──────────────────────────────────────────────────────────────
	log.Printf("[BELLA] --> %s %s", req.Method, req.URL.String())
	for k, vs := range req.Header {
		if maskedHeaders[k] {
			log.Printf("[BELLA]     %s: ***", k)
		} else {
			log.Printf("[BELLA]     %s: %s", k, strings.Join(vs, ", "))
		}
	}

	var reqBody []byte
	if req.Body != nil {
		var err error
		reqBody, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err == nil && len(reqBody) > 0 {
			log.Printf("[BELLA]     body: %s", string(reqBody))
		}
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	// ── Perform ──────────────────────────────────────────────────────────────
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		log.Printf("[BELLA] <-- ERROR: %v", err)
		return nil, err
	}

	// ── Response ─────────────────────────────────────────────────────────────
	log.Printf("[BELLA] <-- %s", resp.Status)
	for k, vs := range resp.Header {
		log.Printf("[BELLA]     %s: %s", k, strings.Join(vs, ", "))
	}

	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("bellabaxter debug: read response body: %w", err)
	}
	if len(respBody) > 0 {
		log.Printf("[BELLA]     body: %s", string(respBody))
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.ContentLength = int64(len(respBody))
	return resp, nil
}
