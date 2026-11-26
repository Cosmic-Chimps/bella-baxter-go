package bellabaxter

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

// Debug output masks key-bearing headers by RULE plus a list, in both directions,
// and masks key-bearing fields of a failure body. The rule and the list are the
// .NET CLI's DebugLoggingHandler; a CLI test holds this list to the CLI's.

// keyHeaders carries every listed header plus names only the rule catches.
// The value of each is a marker that must never appear in the output.
var keyHeaders = map[string]string{
	"Authorization":              "marker-authorization",
	"X-Bella-Key-Id":             "marker-key-id",
	"X-Bella-Signature":          "marker-signature",
	"Cookie":                     "marker-cookie",
	"Set-Cookie":                 "marker-set-cookie",
	"X-Bella-Wrapped-Dek":        "marker-wrapped-dek",
	"X-Bella-Wrapped-Shared-Dek": "marker-wrapped-shared-dek",
	"X-Bella-Lease-Id":           "marker-lease-id",
	"X-Bella-Call-Proof":         "marker-call-proof",
	"X-Bella-Api-Key":            "marker-bella-api-key",
	"X-Vault-Token":              "marker-vault-token",
	"X-Api-Key":                  "marker-api-key",
	"Proxy-Authorization":        "marker-proxy-authorization",
	"X-Identity-Header":          "marker-identity-header",
	"X-New-Device-Key":           "marker-rule-device-key",
	"X-Some-Future-Dek-Header":   "marker-rule-future-dek",
	"X-Client-Credential":        "marker-rule-credential",
	"X-Request-Password":         "marker-rule-password",
}

// headerStub answers with keyHeaders on the response and a chosen status/body.
type headerStub struct {
	status int
	body   string
}

func (s *headerStub) RoundTrip(req *http.Request) (*http.Response, error) {
	h := http.Header{}
	for k, v := range keyHeaders {
		h.Set(k, v)
	}
	h.Set("X-Bella-Lease-Expires", "2026-11-26T00:00:00Z")
	h.Set("X-E2E-Public-Key", "public-key-is-public")
	return &http.Response{
		StatusCode: s.status,
		Status:     http.StatusText(s.status),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

func debugOutput(t *testing.T, status int, respBody string) string {
	t.Helper()
	var sink bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&sink)
	t.Cleanup(func() { log.SetOutput(previous) })

	transport := &loggingRoundTripper{base: &headerStub{status: status, body: respBody}}
	req, err := http.NewRequest(http.MethodGet, "https://example.test/api/v1/projects", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	for k, v := range keyHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("X-Bella-Timestamp", "1759400000")
	req.Header.Set("X-E2E-Public-Key", "public-key-is-public")
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	return sink.String()
}

func TestDebugTransportMasksEveryKeyBearingHeaderRequestAndResponse(t *testing.T) {
	logged := debugOutput(t, http.StatusOK, `{}`)

	for name, value := range keyHeaders {
		if strings.Contains(logged, value) {
			t.Errorf("the value of %s reached the log", name)
		}
		// the name is kept, once for the request and once for the response
		if got := strings.Count(logged, "     "+name+": ***"); got != 2 {
			t.Errorf("%s: want its name logged masked in both directions, got %d", name, got)
		}
	}
	// Not key material: still shown, which is what the output is for.
	for _, want := range []string{
		"X-Bella-Timestamp: 1759400000",
		"X-E2e-Public-Key: public-key-is-public",
		"X-Bella-Lease-Expires: 2026-11-26T00:00:00Z",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("a header that is not key material stopped being logged: %q", want)
		}
	}
}

func TestIsSensitiveHeaderRule(t *testing.T) {
	masked := []string{
		"X-Anything-Key", "x-whatever-TOKEN-v2", "X-Client-Secret", "X-Data-Dek",
		"X-Request-Signature", "X-Session-Cookie", "Proxy-Authorization", "X-Wrapped-Thing",
		"X-Call-Proof", "X-Bella-Wrapped-Dek",
	}
	for _, n := range masked {
		if !isSensitiveHeader(n) {
			t.Errorf("%s should be masked", n)
		}
	}
	visible := []string{
		"X-Bella-Timestamp", "X-User-Role", "X-Bella-Lease-Expires", "Content-Type", "X-E2E-Public-Key", "x-e2e-public-key",
	}
	for _, n := range visible {
		if isSensitiveHeader(n) {
			t.Errorf("%s should stay visible", n)
		}
	}
}

func TestDebugTransportMasksKeyFieldsOfAFailureBody(t *testing.T) {
	body := `{"title":"Forbidden","detail":"diagnostic-text","wrappedDek":"body-dek-marker",` +
		`"nested":{"access_token":"body-token-marker"},"items":[{"signingSecret":"body-token-marker"}]}`

	logged := debugOutput(t, http.StatusForbidden, body)

	if strings.Contains(logged, "body-dek-marker") || strings.Contains(logged, "body-token-marker") {
		t.Fatalf("a key-bearing body field reached the log:\n%s", logged)
	}
	if !strings.Contains(logged, "diagnostic-text") {
		t.Fatalf("the diagnostic part of the failure body was lost:\n%s", logged)
	}
	if !strings.Contains(logged, `"wrappedDek":"***"`) {
		t.Fatalf("the masked field is not shown as masked:\n%s", logged)
	}
}

func TestDebugTransportMasksPairsInANonJSONFailureBody(t *testing.T) {
	logged := debugOutput(t, http.StatusBadGateway, "upstream said refresh_token=body-token-marker&detail=diagnostic-text")

	if strings.Contains(logged, "body-token-marker") {
		t.Fatalf("a key-bearing pair reached the log:\n%s", logged)
	}
	if !strings.Contains(logged, "diagnostic-text") {
		t.Fatalf("the diagnostic part of the failure body was lost:\n%s", logged)
	}
}
