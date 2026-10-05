package bellabaxter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	abs "github.com/microsoft/kiota-abstractions-go"
)

// #1162 — the key is presented on EVERY envelope-required read (apps/sdk/SDK_CONTRACT.md, "Rule: the key
// is presented on every envelope-required read"), and the decrypted body reaches the caller unchanged,
// in that read's own shape. Go presents more broadly (every path containing /secrets); this pins that the
// seven are inside it, including the exports with a query string.
func TestE2EEKeyIsPresentedOnEveryEnvelopeRequiredRead(t *testing.T) {
	item := `{"key":"DATABASE_URL","value":"` + sentinelValue + `","description":null,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","type":null}`
	dict := `{"DATABASE_URL":"` + sentinelValue + `"}`
	reads := []struct{ op, path, plaintext string }{
		{"getAllEnvironmentSecrets", "/api/v1/projects/p/environments/e/secrets",
			`{"environmentSlug":"e","environmentName":"e","secrets":` + dict + `,"version":3,"lastModified":"2026-10-04T00:00:00Z"}`},
		{"exportEnvironmentSecrets", "/api/v1/projects/p/environments/e/secrets/export?format=json", dict},
		{"listSecrets", "/api/v1/projects/p/environments/e/providers/v/secrets", "[" + item + "]"},
		{"exportSecrets", "/api/v1/projects/p/environments/e/providers/v/secrets/export?format=dotenv", dict},
		{"getSecret", "/api/v1/projects/p/environments/e/providers/v/secrets/DATABASE_URL", item},
		{"getSecretVersion", "/api/v1/projects/p/environments/e/providers/v/secrets/DATABASE_URL/versions/2", item},
		{"listGlobalSecrets", "/api/v1/projects/p/secrets",
			`{"projectRef":"p","projectSlug":"p","globalSecretProviderId":null,"secrets":[` + item + `]}`},
	}

	for _, tc := range reads {
		t.Run(tc.op, func(t *testing.T) {
			presented := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				header := r.Header.Get("X-E2E-Public-Key")
				if header == "" {
					// What the API serves a caller that presents no key: plain values over TLS alone.
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(tc.plaintext))
					return
				}
				presented = true
				client, err := parseSPKI(header)
				if err != nil {
					http.Error(w, "bad key", http.StatusBadRequest)
					return
				}
				env, _ := eciesEncrypt([]byte(tc.plaintext), client)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(env)
			}))
			t.Cleanup(srv.Close)

			c := e2eeClient(t, srv.URL)
			ri := abs.NewRequestInformation()
			ri.Method = abs.GET
			ri.UrlTemplate = "{+baseurl}" + tc.path
			ri.PathParameters = map[string]string{"baseurl": srv.URL}
			body, err := c.Generated().RequestAdapter.SendPrimitive(context.Background(), ri, "[]byte", nil)
			if err != nil {
				t.Fatalf("%s: %v", tc.op, err)
			}
			if !presented {
				t.Fatalf("%s: X-E2E-Public-Key was not presented", tc.op)
			}
			if got, _ := body.([]byte); !bytes.Equal(got, []byte(tc.plaintext)) {
				t.Fatalf("%s: decrypted body was reshaped:\n got  %s\n want %s", tc.op, got, tc.plaintext)
			}
		})
	}
}
