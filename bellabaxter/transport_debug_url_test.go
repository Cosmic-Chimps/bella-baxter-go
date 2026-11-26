package bellabaxter

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

// The debug log prints a URL in the request line, in a redirect Location header
// and in an error line. Each goes through maskURLString, which hides the value of
// every query parameter whose NAME is sensitive (the header rule, plus "sig") and
// the userinfo, and leaves everything else as it was.

func TestMaskURLString(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"token", "https://h.test/p?token=abc123", "https://h.test/p?token=***"},
		{"key", "https://h.test/p?key=abc123", "https://h.test/p?key=***"},
		{"signature", "https://h.test/p?signature=abc123", "https://h.test/p?signature=***"},
		{
			"presigned s3",
			"https://b.s3.amazonaws.com/o?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIDEXAMPLE%2F20261126&X-Amz-Date=20261126T000000Z&X-Amz-Expires=300&X-Amz-Security-Token=tok123&X-Amz-SignedHeaders=host&X-Amz-Signature=sig123",
			"https://b.s3.amazonaws.com/o?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=***&X-Amz-Date=20261126T000000Z&X-Amz-Expires=300&X-Amz-Security-Token=***&X-Amz-SignedHeaders=host&X-Amz-Signature=***",
		},
		{
			"azure sas",
			"https://acct.blob.core.windows.net/c/b?sv=2022-11-02&se=2026-12-01T00%3A00%3A00Z&sp=r&sig=abc%2Fdef%3D",
			"https://acct.blob.core.windows.net/c/b?sv=2022-11-02&se=2026-12-01T00%3A00%3A00Z&sp=r&sig=***",
		},
		{"mixed", "https://h.test/p?page=2&token=abc&sort=name&api_key=k", "https://h.test/p?page=2&token=***&sort=name&api_key=***"},
		{"repeated", "https://h.test/p?token=a&x=1&token=b&x=2", "https://h.test/p?token=***&x=1&token=***&x=2"},
		{"encoded name", "https://h.test/p?to%6Ben=abc&x%2Dkey=def&ok=1", "https://h.test/p?to%6Ben=***&x%2Dkey=***&ok=1"},
		{"userinfo", "https://user:pass@h.test:8443/p?a=1", "https://***@h.test:8443/p?a=1"},
		{"userinfo no query", "https://user:pass@h.test/p", "https://***@h.test/p"},
		{"no query", "https://h.test:8443/a/b", "https://h.test:8443/a/b"},
		{"fragment kept", "https://h.test/p?token=abc#frag", "https://h.test/p?token=***#frag"},
		{"relative", "/next?token=abc&n=1", "/next?token=***&n=1"},
		{"empty value", "https://h.test/p?token=&n=1", "https://h.test/p?token=***&n=1"},
		{"at sign in path is not userinfo", "https://h.test/@me?n=1", "https://h.test/@me?n=1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maskURLString(c.in); got != c.want {
				t.Errorf("maskURLString(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

type locationStub struct{ err error }

func (s *locationStub) RoundTrip(req *http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	h := http.Header{}
	h.Set("Location", "https://other.test/cb?code=1&token=redirect-marker&n=2")
	return &http.Response{StatusCode: 302, Status: "302 Found", Header: h, Body: http.NoBody}, nil
}

func TestDebugTransportMasksTheQueryInTheRequestLineAndRedirectLocation(t *testing.T) {
	sink := captureLog(t)
	req, err := http.NewRequest(http.MethodGet, "https://u:p-marker@example.test/x?token=query-marker&page=3", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&loggingRoundTripper{base: &locationStub{}}).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	logged := sink.String()
	for _, leaked := range []string{"query-marker", "p-marker", "redirect-marker"} {
		if strings.Contains(logged, leaked) {
			t.Errorf("%q reached the log:\n%s", leaked, logged)
		}
	}
	for _, want := range []string{
		"--> GET https://***@example.test/x?token=***&page=3",
		"Location: https://other.test/cb?code=1&token=***&n=2",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("missing %q in:\n%s", want, logged)
		}
	}
}

func TestDebugTransportMasksTheURLInAnErrorLine(t *testing.T) {
	sink := captureLog(t)
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/x?signature=err-marker", nil)
	failure := errors.New(`Get "https://example.test/x?signature=err-marker&n=1": connection refused`)
	if _, err := (&loggingRoundTripper{base: &locationStub{err: failure}}).RoundTrip(req); err == nil {
		t.Fatal("want the error back")
	}
	logged := sink.String()
	if strings.Contains(logged, "err-marker") {
		t.Errorf("the error line leaked the query:\n%s", logged)
	}
	if !strings.Contains(logged, "signature=***&n=1") || !strings.Contains(logged, "connection refused") {
		t.Errorf("the error line lost its shape:\n%s", logged)
	}
}

func TestTruncateCutsOnACharacterBoundary(t *testing.T) {
	// "€" is 3 bytes, an emoji 4; every cut position falls inside one of them.
	for _, filler := range []string{"€", "😀"} {
		s := "ab" + strings.Repeat(filler, 10)
		for n := 0; n < len(s); n++ {
			got := truncate(s, n)
			if !utf8.ValidString(got) {
				t.Fatalf("filler %q, n=%d: invalid UTF-8 %q", filler, n, got)
			}
			if !strings.Contains(got, "truncated") {
				t.Fatalf("n=%d: cut without saying so: %q", n, got)
			}
			kept := strings.SplitN(got, "…", 2)[0]
			if len(kept) > n {
				t.Fatalf("n=%d: kept %d bytes", n, len(kept))
			}
		}
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("a short string changed: %q", got)
	}
	if got := truncate("abc€", 6); got != "abc€" {
		t.Errorf("a string at the limit changed: %q", got)
	}
}
