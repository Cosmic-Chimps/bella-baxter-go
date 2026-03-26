#!/usr/bin/env bash
# test-samples.sh — Run all Go SDK samples and verify their output.
#
# Usage:
#   BELLA_API_KEY=bax-... ./test-samples.sh
#   ./test-samples.sh bax-...          # pass key as first argument
#
# Requirements:
#   - bella CLI in PATH  (https://github.com/cosmic-chimps/bella-cli)
#   - go in PATH (1.22+)
#   - Bella Baxter API running at $BELLA_BAXTER_URL (default: http://localhost:5522)

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
BELLA_API_KEY="${1:-${BELLA_API_KEY:-}}"
BELLA_BAXTER_URL="${BELLA_BAXTER_URL:-http://localhost:5522}"

SAMPLES_DIR="$(cd "$(dirname "$0")/samples" && pwd)"

# Server test ports (avoids conflict with Docker Desktop's :8080)
PORT_STDLIB=18080
PORT_GIN=18081

# ── Expected values from demo.env ─────────────────────────────────────────────
EXPECTED_PORT="8080"
EXPECTED_DB_URL="postgres://user:pass@host:port/dbname"
EXPECTED_API_KEY_MASKED="abc1***"     # first 4 chars of abc123xyzAg-FFFx + ***
EXPECTED_APP_ID="550e8400-e29b-41d4-a716-446655440000"
EXPECTED_SETTING1="value1"
EXPECTED_SETTING2="42"
EXPECTED_DB_SCHEME="postgres"

# ── Helpers ───────────────────────────────────────────────────────────────────
PASS=0
FAIL=0

ok() {
  echo "  ✅ $1"
  ((PASS++)) || true
}

fail() {
  echo "  ❌ $1"
  echo "     Expected: $2"
  echo "     Got:      $3"
  ((FAIL++)) || true
}

check() {
  local label="$1"
  local expected="$2"
  local actual="$3"
  if [ "$actual" = "$expected" ]; then
    ok "$label"
  else
    fail "$label" "$expected" "$actual"
  fi
}

contains() {
  local label="$1"
  local needle="$2"
  local haystack="$3"
  if echo "$haystack" | grep -qF "$needle"; then
    ok "$label"
  else
    fail "$label" "(contains) $needle" "$haystack"
  fi
}

extract() {
  # extract value after "KEY    : " pattern
  local key="$1"
  local output="$2"
  echo "$output" | grep -E "^[[:space:]]*${key}[[:space:]]" | head -1 \
    | sed 's/^[^:]*:[[:space:]]*//'
}

require_key() {
  if [ -z "$BELLA_API_KEY" ]; then
    echo "❌  BELLA_API_KEY is not set."
    echo "    Usage: BELLA_API_KEY=bax-... $0"
    echo "           $0 bax-..."
    exit 1
  fi
}

require_bella() {
  if ! command -v bella &>/dev/null; then
    echo "❌  bella CLI not found in PATH."
    exit 1
  fi
}

section() {
  echo ""
  echo "▶ $1"
}

# ── Preflight ─────────────────────────────────────────────────────────────────
require_key
require_bella

export BELLA_BAXTER_URL
export BELLA_API_KEY

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Bella Baxter Go SDK — sample tests"
echo "  API: $BELLA_BAXTER_URL"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# ─────────────────────────────────────────────────────────────────────────────
section "01-dotenv-file  (bella secrets get -o .env && go run .)"
# ─────────────────────────────────────────────────────────────────────────────
cd "$SAMPLES_DIR/01-dotenv-file"
rm -f .env

OUTPUT=$(bella secrets get --api-key "$BELLA_API_KEY" --app "go-01-dotenv-file"  -o .env 2>/dev/null && go run . 2>/dev/null)

contains "01: banner printed"          "=== Bella Baxter: .env file sample (Go) ===" "$OUTPUT"
check    "01: PORT"                    "$EXPECTED_PORT"            "$(extract 'PORT' "$OUTPUT")"
check    "01: DATABASE_URL"            "$EXPECTED_DB_URL"          "$(extract 'DATABASE_URL' "$OUTPUT")"
check    "01: API_KEY masked"          "$EXPECTED_API_KEY_MASKED"  "$(extract 'API_KEY' "$OUTPUT")"
contains "01: .env workflow message"   "bella secrets get -o .env" "$OUTPUT"

rm -f .env

# ─────────────────────────────────────────────────────────────────────────────
section "02-process-inject  (bella run -- go run .)"
# ─────────────────────────────────────────────────────────────────────────────
cd "$SAMPLES_DIR/02-process-inject"

OUTPUT=$(bella run --api-key "$BELLA_API_KEY" --app "go-02-process-inject" -- go run . 2>/dev/null)

contains "02: banner printed"          "=== Bella Baxter: process inject sample (Go) ===" "$OUTPUT"
check    "02: PORT"                    "$EXPECTED_PORT"            "$(extract 'PORT' "$OUTPUT")"
check    "02: DATABASE_URL"            "$EXPECTED_DB_URL"          "$(extract 'DATABASE_URL' "$OUTPUT")"
check    "02: API_KEY masked"          "$EXPECTED_API_KEY_MASKED"  "$(extract 'API_KEY' "$OUTPUT")"
contains "02: process-inject message"  "bella run -- go run ."     "$OUTPUT"

# ─────────────────────────────────────────────────────────────────────────────
section "03-stdlib  (SDK-based net/http server)"
# ─────────────────────────────────────────────────────────────────────────────
cd "$SAMPLES_DIR/03-stdlib"

go build -o /tmp/bella-go-test-03 . 2>/dev/null

BELLA_API_KEY="$BELLA_API_KEY" \
  BELLA_BAXTER_URL="$BELLA_BAXTER_URL" \
  LISTEN_PORT="$PORT_STDLIB" \
  /tmp/bella-go-test-03 2>/tmp/bella-go-03-log.txt &
SRV03_PID=$!

# Wait up to 10s for server to be ready
READY=0
for i in $(seq 1 10); do
  sleep 1
  if grep -q "listening" /tmp/bella-go-03-log.txt 2>/dev/null; then
    READY=1; break
  fi
done

if [ "$READY" -eq 0 ]; then
  fail "03: server did not start in 10s" "listening on :${PORT_STDLIB}" "$(cat /tmp/bella-go-03-log.txt 2>/dev/null)"
else
  contains "03: loaded secrets"     "loaded 8 secret(s)"   "$(cat /tmp/bella-go-03-log.txt)"

  HEALTH=$(curl -s "http://localhost:${PORT_STDLIB}/health" 2>/dev/null)
  check "03: /health → {\"ok\":true}" '{"ok":true}' "$HEALTH"

  CFG_PORT=$(curl -s "http://localhost:${PORT_STDLIB}/config/PORT" 2>/dev/null)
  check "03: /config/PORT value"   '{"key":"PORT","value":"8080"}' \
        "$(echo "$CFG_PORT" | tr -d '\n ')"
fi

kill "$SRV03_PID" 2>/dev/null || true
wait "$SRV03_PID" 2>/dev/null || true
rm -f /tmp/bella-go-test-03 /tmp/bella-go-03-log.txt

# ─────────────────────────────────────────────────────────────────────────────
section "04-gin  (SDK-based Gin server)"
# ─────────────────────────────────────────────────────────────────────────────
cd "$SAMPLES_DIR/04-gin"

go build -o /tmp/bella-go-test-04 . 2>/dev/null

BELLA_API_KEY="$BELLA_API_KEY" \
  BELLA_BAXTER_URL="$BELLA_BAXTER_URL" \
  LISTEN_PORT="$PORT_GIN" \
  /tmp/bella-go-test-04 >/tmp/bella-go-04-log.txt 2>&1 &
SRV04_PID=$!

READY=0
for i in $(seq 1 10); do
  sleep 1
  if grep -q "listening" /tmp/bella-go-04-log.txt 2>/dev/null; then
    READY=1; break
  fi
done

if [ "$READY" -eq 0 ]; then
  fail "04: server did not start in 10s" "listening on :${PORT_GIN}" "$(cat /tmp/bella-go-04-log.txt 2>/dev/null)"
else
  contains "04: loaded secrets"     "loaded 8 secret(s)"   "$(cat /tmp/bella-go-04-log.txt)"

  HEALTH=$(curl -s "http://localhost:${PORT_GIN}/health" 2>/dev/null)
  check "04: /health → {\"ok\":true}" '{"ok":true}' "$HEALTH"

  CFG_PORT=$(curl -s "http://localhost:${PORT_GIN}/config/PORT" 2>/dev/null)
  check "04: /config/PORT value"   '{"key":"PORT","value":"8080"}' \
        "$(echo "$CFG_PORT" | tr -d '\n ')"
fi

kill "$SRV04_PID" 2>/dev/null || true
wait "$SRV04_PID" 2>/dev/null || true
rm -f /tmp/bella-go-test-04 /tmp/bella-go-04-log.txt

# ─────────────────────────────────────────────────────────────────────────────
section "05-typed-secrets  (bella run -- go run .)"
# ─────────────────────────────────────────────────────────────────────────────
cd "$SAMPLES_DIR/05-typed-secrets"

OUTPUT=$(bella run --api-key "$BELLA_API_KEY" --app "go-05-typed-secrets" -- go run . 2>/dev/null)

contains "05: banner printed"            "=== Bella Baxter: Typed Secrets (Go) ===" "$OUTPUT"

# String
API_KEY_VAL=$(extract 'String  EXTERNAL_API_KEY' "$OUTPUT")
check    "05: String EXTERNAL_API_KEY"   "$EXPECTED_API_KEY_MASKED" "$API_KEY_VAL"

# Int
PORT_VAL=$(extract 'Int     PORT' "$OUTPUT" | sed 's/[[:space:]]*←.*//')
check    "05: Int PORT"                  "$EXPECTED_PORT" "$(echo "$PORT_VAL" | xargs)"

# Bool
BOOL_VAL=$(extract 'Bool    ENABLE_FEATURES' "$OUTPUT" | sed 's/[[:space:]]*←.*//')
check    "05: Bool ENABLE_FEATURES"      "true" "$(echo "$BOOL_VAL" | xargs)"

# Uri
URI_VAL=$(extract 'Uri     DATABASE_URL' "$OUTPUT" | sed 's/[[:space:]]*←.*//')
check    "05: Uri DATABASE_URL scheme"   "scheme=${EXPECTED_DB_SCHEME}" "$(echo "$URI_VAL" | xargs)"

# JSON struct
JSON_SETTING1=$(extract '\.Setting1' "$OUTPUT" | sed 's/[[:space:]]*←.*//' | tr -d '"')
check    "05: JSON APP_CONFIG.Setting1"  "$EXPECTED_SETTING1" "$(echo "$JSON_SETTING1" | xargs)"

JSON_SETTING2=$(extract '\.Setting2' "$OUTPUT" | sed 's/[[:space:]]*←.*//')
check    "05: JSON APP_CONFIG.Setting2"  "$EXPECTED_SETTING2" "$(echo "$JSON_SETTING2" | xargs)"

# GUID
GUID_VAL=$(extract 'GUID    APP_ID' "$OUTPUT" | sed 's/[[:space:]]*←.*//')
check    "05: GUID APP_ID"               "$EXPECTED_APP_ID" "$(echo "$GUID_VAL" | xargs)"

contains "05: no-raw-getenv message"     "No raw os.Getenv calls" "$OUTPUT"

# ─────────────────────────────────────────────────────────────────────────────
# Results
# ─────────────────────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
TOTAL=$((PASS + FAIL))
if [ "$FAIL" -eq 0 ]; then
  echo "  ✅  All ${TOTAL} checks passed."
else
  echo "  ❌  ${FAIL} of ${TOTAL} checks failed (${PASS} passed)."
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

exit "$FAIL"
