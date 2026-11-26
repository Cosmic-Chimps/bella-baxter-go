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
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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
	pubB64       string                                                                 // base64-encoded SPKI public key, sent as header
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

// loadPrivateKeyPEM parses a PKCS#8 P-256 private key and returns it as *ecdh.PrivateKey.
//
// It accepts the PKCS#8 PEM `bella sdk run` injects and bare base64 PKCS#8 DER: when the value
// holds no PEM block, the `-----…-----` armour and all whitespace (including CRLF) are stripped
// and the rest is base64-decoded — the same rule the JS, Java, .NET, Dart and Swift SDKs apply to
// BELLA_BAXTER_PRIVATE_KEY. Both *ecdsa.PrivateKey and *ecdh.PrivateKey PKCS#8 encodings load.
//
// Any other curve is refused: the wire contract is P-256 only, and a P-384 key would otherwise
// load here and fail later as an opaque server-side error.
func loadPrivateKeyPEM(pemStr string) (*ecdh.PrivateKey, error) {
	var der []byte
	if block, _ := pem.Decode([]byte(pemStr)); block != nil {
		der = block.Bytes
	} else {
		body := pemArmourOrSpace.ReplaceAllString(pemStr, "")
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return nil, fmt.Errorf("neither a PEM block nor base64 PKCS#8 DER")
		}
		der = decoded
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS#8: %w", err)
	}
	var priv *ecdh.PrivateKey
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		if priv, err = k.ECDH(); err != nil {
			return nil, err
		}
	case *ecdh.PrivateKey:
		priv = k
	default:
		return nil, fmt.Errorf("unsupported key type %T, expected a P-256 EC key", key)
	}
	if priv.Curve() != ecdh.P256() {
		return nil, fmt.Errorf("unsupported curve, expected P-256")
	}
	return priv, nil
}

// pemArmourOrSpace matches `-----BEGIN …-----` / `-----END …-----` lines and whitespace.
var pemArmourOrSpace = regexp.MustCompile(`-----[A-Z0-9 ]+-----|\s`)

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
	// #1050 (b): the key was presented, so on a read the server always encrypts, plaintext is refused.
	mustBeEnvelope := isSecretsReq && requiresEnvelope(req.Method, req.URL.Path)

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
		if mustBeEnvelope {
			// Not an envelope where one is the only acceptable answer: never hand it to the caller.
			return nil, newE2EEResponseError(E2EEPlaintextResponse, req.URL.Path, nil)
		}
		// A value-less /secrets call (metadata, writes, …) is answered in plain JSON: pass it through.
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
	} else {
		secrets, err := t.decrypt(envelope.ServerPublicKey, envelope.Nonce, envelope.Tag, envelope.Ciphertext)
		if err != nil {
			return nil, newE2EEResponseError(E2EEDecryptionFailed, req.URL.Path, err)
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
// Key-bearing headers are masked, request and response alike: the ones on
// maskedHeaders, and any header whose NAME contains one of sensitiveNameParts
// (key, token, secret, dek, signature, cookie, authorization, wrapped, proof,
// password, credential), so a key header added tomorrow is hidden by default.
// X-E2E-Public-Key, the PUBLIC key the client sends, is the one exception. The
// rule and the list are the .NET CLI's (DebugLoggingHandler), which a CLI test
// holds this list to. URLs are masked too (maskURLString): the value of every
// query parameter whose NAME is sensitive, userinfo and all, wherever the log
// prints one. BODIES ARE NOT LOGGED except for a truncated response
// body on a non-2xx status, with its key-bearing fields masked (redactBody).
//
// The body rule is not caution, it is the whole point. This transport is the
// OUTERMOST of the chain (debug -> HMAC -> optional E2EE -> default), so it sees
// request bodies before encryption and response bodies after decryption.
// CreateSecret and UpdateSecret put the secret value in the request body in the
// clear, so logging request bodies wrote secrets to stderr for anyone who had set
// BELLA_DEBUG=1 — including a `terraform apply` that merely inherited it from its
// environment. A successful response body is the read path and carries the value
// for the same reason.
//
// A failure body is the one worth keeping: it is the API's problem document, it is
// what a person turns debug logging on to read, and it does not carry a secret
// value. It is truncated, matching the .NET CLI's DebugLoggingHandler, which has
// had this shape all along.
type loggingRoundTripper struct {
	base http.RoundTripper
}

// maxLoggedBody is how much of a failure body reaches the log. Long enough for a
// problem document, short enough that an unexpected payload cannot scroll away
// whatever the reader was actually looking for.
const maxLoggedBody = 500

// Keys are in net/http canonical form, which is what Header maps are keyed by.
// Keep every entry a plain `"Name": true,` line: the CLI's parity test reads this literal.
var maskedHeaders = map[string]bool{
	"Authorization":              true,
	"X-Bella-Key-Id":             true,
	"X-Bella-Signature":          true,
	"Cookie":                     true,
	"Set-Cookie":                 true,
	"X-Bella-Wrapped-Dek":        true,
	"X-Bella-Wrapped-Shared-Dek": true,
	"X-Bella-Lease-Id":           true,
	"X-Bella-Call-Proof":         true,
	"X-Bella-Api-Key":            true,
	"X-Vault-Token":              true,
	"X-Api-Key":                  true,
	"X-Identity-Header":          true,
	"Proxy-Authorization":        true,
}

// sensitiveNameParts: a header (or body field) whose name contains one of these,
// compared case-insensitively, is masked even when it is on no list.
var sensitiveNameParts = []string{
	"key", "token", "secret", "dek", "signature", "cookie", "authorization",
	"wrapped", "proof", "password", "credential",
}

// neverMaskedHeaders is what the rule would catch that is not secret: the E2E
// PUBLIC key the client sends so the server can wrap a data key to it.
var neverMaskedHeaders = map[string]bool{
	"X-E2e-Public-Key": true, // canonical form of X-E2E-Public-Key
}

// isSensitiveHeader reports whether the value of the header called name must never be logged.
func isSensitiveHeader(name string) bool {
	canonical := http.CanonicalHeaderKey(name)
	if neverMaskedHeaders[canonical] {
		return false
	}
	if maskedHeaders[canonical] {
		return true
	}
	return nameIsSensitive(name)
}

func nameIsSensitive(name string) bool {
	lower := strings.ToLower(name)
	for _, part := range sensitiveNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

// isSensitiveField is the body-field form of the rule: snake_case, kebab-case and
// camelCase reduce to the same letters (wrapped_dek, wrappedDek, wrapped-dek).
func isSensitiveField(name string) bool {
	return nameIsSensitive(strings.NewReplacer("_", "", "-", "").Replace(name))
}

// sensitivePair finds name=value / name: value pairs with a sensitive name in a
// body that is not JSON (a proxy's page, a form echo).
var sensitivePair = regexp.MustCompile(
	`(?i)([\w-]*(?:key|token|secret|dek|signature|password|wrapped|proof|credential)[\w-]*)(\s*[=:]\s*)(?:"[^"]*"|[^\s&,;"]+)`)

// redactBody returns a failure body with every key-bearing field masked: a JSON
// body keeps its shape and loses the value of each property whose name matches the
// rule (whatever type the value is); anything else gets its name=value pairs masked.
func redactBody(body string) string {
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var node interface{}
	if err := dec.Decode(&node); err == nil && !dec.More() {
		switch node.(type) {
		case map[string]interface{}, []interface{}:
			redactNode(node)
			var out bytes.Buffer
			enc := json.NewEncoder(&out)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(node); err == nil {
				return strings.TrimSuffix(out.String(), "\n")
			}
		}
	}
	return sensitivePair.ReplaceAllString(body, "${1}${2}***")
}

func redactNode(node interface{}) {
	switch n := node.(type) {
	case map[string]interface{}:
		for k, v := range n {
			if isSensitiveField(k) {
				n[k] = "***"
			} else {
				redactNode(v)
			}
		}
	case []interface{}:
		for _, v := range n {
			redactNode(v)
		}
	}
}

// sensitiveQueryNames are the query parameters the name rule misses. "sig" is the
// signature of an Azure SAS URL (the rest of a SAS, se/sv/sp, is expiry, version
// and permissions: harmless, and what a person debugging an expired link needs to
// read), so it is the one name added. The presigned S3 names are all caught by the
// rule itself (X-Amz-Signature, X-Amz-Credential, X-Amz-Security-Token).
var sensitiveQueryNames = map[string]bool{"sig": true}

// urlHeaders carry a URL, which can carry a credential in its query.
var urlHeaders = map[string]bool{"Location": true, "Content-Location": true, "Referer": true}

// authorityPrefix matches the start of an absolute or scheme-relative URL.
var authorityPrefix = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9+.-]*:)?//`)

// urlInText finds absolute URLs inside free text, such as an error message.
var urlInText = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)

func isSensitiveQueryName(name string) bool {
	if decoded, err := url.QueryUnescape(name); err == nil {
		name = decoded
	}
	return nameIsSensitive(name) || sensitiveQueryNames[strings.ToLower(name)]
}

// maskURLString returns s with the value of every sensitive query parameter and
// the userinfo replaced by ***. Scheme, host, port, path, fragment, the other
// parameters, their order and repeated names are kept; a URL without a query or
// userinfo comes back unchanged. It works on the text, not on a parsed URL, so a
// string that does not parse is masked the same way.
func maskURLString(s string) string {
	rest, frag := s, ""
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest, frag = rest[:i], rest[i:]
	}
	query, hasQuery := "", false
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest, query, hasQuery = rest[:i], rest[i+1:], true
	}
	if loc := authorityPrefix.FindStringIndex(rest); loc != nil {
		authEnd := len(rest)
		if i := strings.IndexByte(rest[loc[1]:], '/'); i >= 0 {
			authEnd = loc[1] + i
		}
		if at := strings.LastIndexByte(rest[loc[1]:authEnd], '@'); at >= 0 {
			rest = rest[:loc[1]] + "***@" + rest[loc[1]+at+1:]
		}
	}
	if !hasQuery {
		return rest + frag
	}
	pairs := strings.Split(query, "&")
	for i, pair := range pairs {
		name, _, hasValue := strings.Cut(pair, "=")
		if hasValue && isSensitiveQueryName(name) {
			pairs[i] = name + "=***"
		}
	}
	return rest + "?" + strings.Join(pairs, "&") + frag
}

// maskURLsInText masks every absolute URL found in free text.
func maskURLsInText(s string) string {
	return urlInText.ReplaceAllStringFunc(s, maskURLString)
}

func logHeaders(h http.Header) {
	for k, vs := range h {
		if isSensitiveHeader(k) {
			log.Printf("[BELLA]     %s: ***", k)
		} else if urlHeaders[http.CanonicalHeaderKey(k)] {
			masked := make([]string, len(vs))
			for i, v := range vs {
				masked[i] = maskURLString(v)
			}
			log.Printf("[BELLA]     %s: %s", k, strings.Join(masked, ", "))
		} else {
			log.Printf("[BELLA]     %s: %s", k, strings.Join(vs, ", "))
		}
	}
}

func (t *loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// ── Request ──────────────────────────────────────────────────────────────
	log.Printf("[BELLA] --> %s %s", req.Method, maskURLString(req.URL.String()))
	logHeaders(req.Header)

	// The request body is never logged: on a write it IS the secret value.

	// ── Perform ──────────────────────────────────────────────────────────────
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		log.Printf("[BELLA] <-- ERROR: %s", maskURLsInText(err.Error()))
		return nil, err
	}

	// ── Response ─────────────────────────────────────────────────────────────
	log.Printf("[BELLA] <-- %s", resp.Status)
	logHeaders(resp.Header)

	// A successful body is a secret on the read path, so it is never logged. The
	// body is still read and replaced either way: draining it here and handing back
	// a fresh reader is what keeps the caller's decoding identical with and without
	// debug logging on.
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("bellabaxter debug: read response body: %w", err)
	}
	if !isOK(resp) && len(respBody) > 0 {
		log.Printf("[BELLA]     body: %s", truncate(redactBody(string(respBody)), maxLoggedBody))
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.ContentLength = int64(len(respBody))
	return resp, nil
}

// truncate shortens s to at most n bytes, saying so when it did.
//
// The cut is made on a character boundary: it steps back to the start of the rune
// it would have split, so a multi-byte character straddling the limit is dropped
// whole instead of leaving an invalid UTF-8 tail in the log. It marks the cut so a
// reader never mistakes a truncated document for a malformed one.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("… (%d bytes total, truncated)", len(s))
}
