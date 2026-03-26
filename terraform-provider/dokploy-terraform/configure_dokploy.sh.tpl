#!/bin/bash
set -euo pipefail

# ─────────────────────────────────────────────
# Variables injected by Terraform templatefile()
# ─────────────────────────────────────────────
ADMIN_EMAIL="${dokploy_admin_email}"
ADMIN_PASSWORD="${dokploy_admin_password}"
GITHUB_TOKEN="${github_token}"

DOKPLOY_URL="http://localhost:3000"
API="$DOKPLOY_URL/api"

# ─────────────────────────────────────────────
# Helper: retry a command up to N times
# ─────────────────────────────────────────────
retry() {
  local n=0
  local max=$1; shift
  local delay=$2; shift
  until "$@"; do
    n=$((n+1))
    if [ "$n" -ge "$max" ]; then
      echo "ERROR: command failed after $max attempts: $*"
      return 1
    fi
    echo "Attempt $n/$max failed — retrying in $${delay}s..."
    sleep "$delay"
  done
}

# ─────────────────────────────────────────────
# 1. Wait for Dokploy API to be reachable
# ─────────────────────────────────────────────
echo ">>> Waiting for Dokploy to become ready..."
retry 40 15 curl -sf --max-time 5 "$API/health" > /dev/null
echo ">>> Dokploy is up."

# ─────────────────────────────────────────────
# 2. Bootstrap admin account (first-run only)
# ─────────────────────────────────────────────
echo ">>> Setting up admin account..."
SETUP_RESPONSE=$(curl -sf -X POST "$API/auth.createAdmin" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" || echo "SKIP")

if echo "$SETUP_RESPONSE" | grep -qi "already"; then
  echo ">>> Admin already exists, skipping."
fi

# ─────────────────────────────────────────────
# 3. Authenticate and get token
# ─────────────────────────────────────────────
echo ">>> Authenticating..."
AUTH_RESPONSE=$(curl -sf -X POST "$API/auth.login" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}")

TOKEN=$(echo "$AUTH_RESPONSE" | jq -r '.token')
if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
  echo "ERROR: Failed to retrieve auth token. Response: $AUTH_RESPONSE"
  exit 1
fi
echo ">>> Authenticated successfully."

AUTH_HEADER="Authorization: Bearer $TOKEN"

# ─────────────────────────────────────────────
# 4. Optionally register GitHub token as a
#    git provider so private repos work
# ─────────────────────────────────────────────
if [ -n "$GITHUB_TOKEN" ]; then
  echo ">>> Registering GitHub token..."
  curl -sf -X POST "$API/gitProvider.createGithub" \
    -H "Content-Type: application/json" \
    -H "$AUTH_HEADER" \
    -d "{\"name\":\"github\",\"accessToken\":\"$GITHUB_TOKEN\"}" || true
  echo ">>> GitHub token registered."
fi

# ─────────────────────────────────────────────
# 5. Create a Dokploy project to group the apps
# ─────────────────────────────────────────────
echo ">>> Creating project..."
PROJECT_RESPONSE=$(curl -sf -X POST "$API/project.create" \
  -H "Content-Type: application/json" \
  -H "$AUTH_HEADER" \
  -d '{"name":"terraform-apps","description":"Apps provisioned by Terraform"}')

PROJECT_ID=$(echo "$PROJECT_RESPONSE" | jq -r '.projectId')
echo ">>> Project ID: $PROJECT_ID"

# ─────────────────────────────────────────────
# 6. Register each Docker Compose app
# ─────────────────────────────────────────────
%{ for app in docker_compose_apps ~}
echo ">>> Registering app: ${app.name}"

COMPOSE_RESPONSE=$(curl -sf -X POST "$API/compose.create" \
  -H "Content-Type: application/json" \
  -H "$AUTH_HEADER" \
  -d "{
    \"name\":        \"${app.name}\",
    \"description\": \"${app.description}\",
    \"projectId\":   \"$PROJECT_ID\"
  }")

COMPOSE_ID=$(echo "$COMPOSE_RESPONSE" | jq -r '.composeId')
echo ">>>   Compose ID: $COMPOSE_ID"

# Attach the GitHub repo
curl -sf -X POST "$API/compose.update" \
  -H "Content-Type: application/json" \
  -H "$AUTH_HEADER" \
  -d "{
    \"composeId\":    \"$COMPOSE_ID\",
    \"sourceType\":   \"github\",
    \"repository\":   \"${app.repo_url}\",
    \"branch\":       \"${app.branch}\",
    \"composeFile\":  \"${app.compose_file}\"
  }" > /dev/null

echo ">>>   Repo attached: ${app.repo_url} @ ${app.branch}"

# Trigger initial deploy
curl -sf -X POST "$API/compose.deploy" \
  -H "Content-Type: application/json" \
  -H "$AUTH_HEADER" \
  -d "{\"composeId\":\"$COMPOSE_ID\"}" > /dev/null

echo ">>>   Deploy triggered for ${app.name}"
%{ endfor ~}

echo ""
echo "════════════════════════════════════════════"
echo "  ✅  Dokploy configuration complete!"
echo "  🌐  UI: $DOKPLOY_URL"
echo "  📧  Login: $ADMIN_EMAIL"
echo "════════════════════════════════════════════"
