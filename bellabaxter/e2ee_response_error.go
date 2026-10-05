package bellabaxter

import (
	"fmt"
	"net/http"
	"strings"
)

// Error codes of an E2EEResponseError. They are the cross-SDK wire contract
// (apps/sdk/SDK_CONTRACT.md, "Rule: a presented key requires an envelope") and never change.
const (
	// E2EEPlaintextResponse: the key was presented on a read the server always encrypts, and the
	// 2xx answer was not an envelope (plain secrets, or not JSON at all).
	E2EEPlaintextResponse = "e2ee-plaintext-response"
	// E2EEDecryptionFailed: the answer was an envelope ("encrypted": true) that did not decrypt —
	// a missing or undecodable field, a failed GCM tag (tampered), or encrypted to another key.
	E2EEDecryptionFailed = "e2ee-decryption-failed"
)

// E2EEResponseError is returned instead of a secrets response once this client has presented its
// E2EE public key and the answer is not an envelope that decrypts with that key (#1050). There is no
// plaintext fallback: a header-stripping intermediary or a server regression would otherwise hand the
// caller unencrypted secrets it believes were end-to-end encrypted.
//
// The message names the request path and the code, never the body, ciphertext or key material. The
// decryption cause, when there is one, is available through errors.Unwrap. Match with errors.As.
type E2EEResponseError struct {
	Code  string // E2EEPlaintextResponse or E2EEDecryptionFailed
	Path  string // request path of the refused response
	cause error
}

func newE2EEResponseError(code, path string, cause error) *E2EEResponseError {
	return &E2EEResponseError{Code: code, Path: path, cause: cause}
}

func (e *E2EEResponseError) Error() string {
	if e.Code == E2EEPlaintextResponse {
		return fmt.Sprintf("E2EE response expected but plaintext received for %s; refusing it (%s)", e.Path, e.Code)
	}
	return fmt.Sprintf("E2EE response could not be decrypted for %s; refusing it (%s)", e.Path, e.Code)
}

// Unwrap returns the decryption failure behind an E2EEDecryptionFailed error, or nil.
func (e *E2EEResponseError) Unwrap() error { return e.cause }

// requiresEnvelope reports whether the server ALWAYS encrypts a 2xx answer to this request when the
// key is presented: the GETs that carry secret values (SDK_CONTRACT.md, "Which Endpoints Support
// E2EE"). Every other /secrets call (metadata, versions list, hash, manifest, writes) is answered in
// plain JSON, so requiring an envelope there would break it.
func requiresEnvelope(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	const marker = "/api/v1/projects/"
	i := strings.Index(path, marker)
	if i < 0 {
		return false
	}
	segs := strings.Split(path[i+len(marker):], "/")
	if len(segs) < 2 || segs[0] == "" {
		return false
	}
	rest := segs[1:]
	switch {
	case len(rest) == 1:
		return rest[0] == "secrets" // listGlobalSecrets
	case len(rest) < 3 || rest[0] != "environments" || rest[1] == "":
		return false
	}
	env := rest[2:]
	switch len(env) {
	case 1:
		return env[0] == "secrets" // getAllEnvironmentSecrets
	case 2:
		return env[0] == "secrets" && env[1] == "export" // exportEnvironmentSecrets
	}
	if env[0] != "providers" || env[1] == "" || env[2] != "secrets" {
		return false
	}
	prov := env[3:]
	switch len(prov) {
	case 0:
		return true // listSecrets
	case 1:
		return prov[0] != "" && prov[0] != "hash" // exportSecrets, getSecret
	case 3:
		return prov[0] != "" && prov[1] == "versions" && allDigits(prov[2]) // getSecretVersion
	}
	return false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
