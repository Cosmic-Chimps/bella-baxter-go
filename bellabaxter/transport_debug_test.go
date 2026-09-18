package bellabaxter

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
)

// #825 — BELLA_DEBUG must not write secret values to stderr.
//
// The debug transport is the OUTERMOST of the chain (debug -> HMAC -> optional
// E2EE -> default), so it sees request bodies before encryption and response
// bodies after decryption. CreateSecret and UpdateSecret carry the value in the
// request body in the clear. Logging bodies therefore printed secrets to stderr
// for any process with BELLA_DEBUG=1 set — including a `terraform apply` that
// inherited it — while the option's own documentation promised masking.
//
// These drive the real RoundTrip against a stub and read what was logged, so they
// assert the observable behaviour rather than the shape of the code.

// fakeRoundTripper answers with a fixed status and body.
type fakeRoundTripper struct {
	status int
	body   string
	seen   []byte // the request body as the next hop received it
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		f.seen, _ = io.ReadAll(req.Body)
	}
	return &http.Response{
		StatusCode: f.status,
		Status:     http.StatusText(f.status),
		Header:     http.Header{"Set-Cookie": []string{"session=abc"}},
		Body:       io.NopCloser(strings.NewReader(f.body)),
	}, nil
}

// roundTrip runs one request through the debug transport and returns what it logged.
func roundTrip(t *testing.T, reqBody, respBody string, status int) (logged string, resp *http.Response, next *fakeRoundTripper) {
	t.Helper()

	var sink bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&sink)
	t.Cleanup(func() { log.SetOutput(previous) })

	next = &fakeRoundTripper{status: status, body: respBody}
	transport := &loggingRoundTripper{base: next}

	req, err := http.NewRequest(http.MethodPost, "https://example.test/api/v1/secrets", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer super-secret-token")
	req.Header.Set("X-App-Client", "unit-test")

	resp, err = transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	return sink.String(), resp, next
}

func TestDebugTransportNeverLogsTheRequestBody(t *testing.T) {
	const secret = "hunter2-the-actual-secret-value"

	logged, _, next := roundTrip(t, `{"key":"DB_PASSWORD","value":"`+secret+`"}`, `{"id":"1"}`, http.StatusOK)

	if strings.Contains(logged, secret) {
		t.Fatalf("the secret value reached the log:\n%s", logged)
	}
	// Still delivered: the transport must not consume the body it declines to log.
	if !strings.Contains(string(next.seen), secret) {
		t.Fatalf("the request body did not reach the next hop: %q", next.seen)
	}
}

func TestDebugTransportNeverLogsASuccessfulResponseBody(t *testing.T) {
	const secret = "value-returned-by-the-read-path"

	logged, resp, _ := roundTrip(t, "", `{"value":"`+secret+`"}`, http.StatusOK)

	if strings.Contains(logged, secret) {
		t.Fatalf("a decrypted response value reached the log:\n%s", logged)
	}
	// And the caller can still read it.
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), secret) {
		t.Fatalf("the response body was consumed: %q", body)
	}
}

func TestDebugTransportLogsAFailureBodyTruncated(t *testing.T) {
	// The case debug logging exists for: a problem document, which carries no value.
	long := `{"title":"Bad Request","detail":"` + strings.Repeat("x", maxLoggedBody*2) + `"}`

	logged, _, _ := roundTrip(t, "", long, http.StatusBadRequest)

	if !strings.Contains(logged, "Bad Request") {
		t.Fatalf("the failure body was not logged at all:\n%s", logged)
	}
	if !strings.Contains(logged, "truncated") {
		t.Fatalf("a body over the limit was logged whole:\n%s", logged)
	}
	if strings.Contains(logged, strings.Repeat("x", maxLoggedBody+1)) {
		t.Fatalf("more than the limit was logged:\n%s", logged)
	}
}

func TestDebugTransportMasksCredentialHeadersBothWays(t *testing.T) {
	logged, _, _ := roundTrip(t, "", `{}`, http.StatusOK)

	if strings.Contains(logged, "super-secret-token") {
		t.Fatalf("the Authorization header reached the log:\n%s", logged)
	}
	if strings.Contains(logged, "session=abc") {
		t.Fatalf("a Set-Cookie response header reached the log:\n%s", logged)
	}
	// A non-sensitive header is still shown, or the log is useless.
	if !strings.Contains(logged, "unit-test") {
		t.Fatalf("ordinary headers stopped being logged:\n%s", logged)
	}
}
