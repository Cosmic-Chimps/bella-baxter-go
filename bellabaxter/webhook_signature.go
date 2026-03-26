package bellabaxter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// VerifyWebhookSignature verifies the X-Bella-Signature header on a received
// Bella Baxter webhook request.
//
// secret is the whsec-xxx signing secret.
// signatureHeader is the raw value of the X-Bella-Signature header,
// e.g. "t=1714000000,v1=abc123...".
// rawBody is the unmodified request body bytes.
// toleranceSeconds is the maximum age of the timestamp; pass 300 for the
// default 5-minute replay-protection window.
//
// Returns (true, nil) when the signature is valid and within the tolerance
// window. Returns (false, nil) when the signature is invalid or the timestamp
// is stale. Returns (false, error) only when the header is malformed.
func VerifyWebhookSignature(secret, signatureHeader string, rawBody []byte, toleranceSeconds int64) (bool, error) {
	// Parse t= and v1= from header
	var timestamp int64
	var expectedSig string

	for _, part := range strings.Split(signatureHeader, ",") {
		idx := strings.IndexByte(part, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		switch key {
		case "t":
			var err error
			timestamp, err = strconv.ParseInt(value, 10, 64)
			if err != nil {
				return false, fmt.Errorf("bellabaxter webhook: invalid timestamp %q: %w", value, err)
			}
		case "v1":
			expectedSig = value
		}
	}

	if timestamp == 0 || expectedSig == "" {
		return false, fmt.Errorf("bellabaxter webhook: missing t or v1 in signature header")
	}

	// Check timestamp tolerance (replay-attack protection)
	diff := time.Now().Unix() - timestamp
	if diff < -toleranceSeconds || diff > toleranceSeconds {
		return false, nil
	}

	// HMAC-SHA256: key=UTF8(secret), data=UTF8("{t}.{rawBody}")
	signingInput := strconv.FormatInt(timestamp, 10) + "." + string(rawBody)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	computedMAC := mac.Sum(nil)

	// Decode expected signature from hex for constant-time byte comparison
	expectedMAC, err := hex.DecodeString(expectedSig)
	if err != nil {
		return false, fmt.Errorf("bellabaxter webhook: invalid v1 hex: %w", err)
	}

	// Timing-safe compare of raw HMAC bytes
	return hmac.Equal(computedMAC, expectedMAC), nil
}
