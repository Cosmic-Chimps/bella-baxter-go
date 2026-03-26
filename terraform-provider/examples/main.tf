terraform {
  required_providers {
    bella = {
      source  = "cosmic-chimps/bella-baxter"
      version = "~> 0.1"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

# ── Provider configuration ────────────────────────────────────────────────────
# Auth: API key (starts with "bax-"). Obtain from WebApp → Project → API Keys.
#
# The API key is already scoped to a specific project + environment.
# project_slug and environment_slug are resolved automatically via GET /api/v1/keys/me.
#
# All attributes can be set via environment variables instead:
#   BELLA_BAXTER_URL  — API base URL
#   BELLA_API_KEY     — API key
provider "bella" {
  baxter_url = "https://baxter.example.com"
  api_key    = var.bella_api_key
}

variable "bella_api_key" {
  description = "Bella Baxter API key (starts with 'bax-'). Obtain from WebApp → Project → API Keys."
  type        = string
  sensitive   = true
}

# ── Data sources ──────────────────────────────────────────────────────────────

# Read a single secret — project+env from API key context
data "bella_secret" "db_password" {
  key = "RDS_PASSWORD"
}

# Read ALL secrets — project+env from API key context
data "bella_secrets" "all" {}

# Access individual secrets from the map
output "db_url" {
  value     = data.bella_secrets.all.secrets["DATABASE_URL"]
  sensitive = true
}

output "secrets_version" {
  value = data.bella_secrets.all.version
}

# ── Resource: manage a secret ─────────────────────────────────────────────────

# Generate a random password for RDS
resource "random_password" "rds" {
  length  = 32
  special = false
}

# Store the generated password as a Bella Baxter secret.
# provider_slug identifies which secret backend within the environment to write to.
# project_slug and environment_slug are resolved from the API key context.
resource "bella_secret" "rds_password" {
  provider_slug = "my-vault"
  key           = "RDS_PASSWORD"
  value         = random_password.rds.result
  description   = "RDS master password — managed by Terraform"
}

# Another secret referencing a Terraform variable
variable "api_token" {
  type      = string
  sensitive = true
}

resource "bella_secret" "external_api_token" {
  provider_slug = "my-vault"
  key           = "EXTERNAL_API_TOKEN"
  value         = var.api_token
  description   = "Third-party API token"
}

# ── Outputs ───────────────────────────────────────────────────────────────────

output "rds_secret_id" {
  value = bella_secret.rds_password.id
}

output "db_password_from_data_source" {
  value     = data.bella_secret.db_password.value
  sensitive = true
}
