# Bella Baxter Terraform Provider

A Terraform provider for [Bella Baxter](https://github.com/cosmic-chimps/bella-baxter) — a secret management gateway that proxies CRUD operations to external secret backends (HashiCorp Vault / OpenBao, AWS Secrets Manager, Azure Key Vault, GCP Secret Manager).

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) ≥ 1.5
- [Go](https://golang.org/) ≥ 1.22 (to build from source)
- A running Bella Baxter API instance

## Provider Configuration

```hcl
terraform {
  required_providers {
    bella = {
      source  = "cosmic-chimps/bella-baxter"
      version = "~> 0.1"
    }
  }
}

provider "bella" {
  baxter_url = "https://baxter.example.com"   # or BELLA_BAXTER_URL env var
  api_key    = var.bella_api_key               # or BELLA_API_KEY env var (sensitive)
}
```

### Environment Variables

| Variable           | Description                              |
|--------------------|------------------------------------------|
| `BELLA_BAXTER_URL` | Base URL of the Bella Baxter API         |
| `BELLA_API_KEY`    | API key (starts with `bax-`, sensitive)  |

> **Note:** The API key already encodes which project and environment it is scoped to.
> `project_slug` and `environment_slug` are resolved automatically from the key via `GET /api/v1/keys/me` — you never need to specify them.

---

## Resources

### `bella_secret`

Manages a single secret within a Bella Baxter environment. The secret is stored in the external provider (Vault, AWS, etc.) — not in Terraform state.

All project and environment context is resolved automatically from the API key via `GET /api/v1/keys/me` — you never need to specify `project_slug` or `environment_slug`.

#### Example

```hcl
resource "random_password" "rds" {
  length  = 32
  special = false
}

resource "bella_secret" "rds_password" {
  provider_slug = "my-vault"
  key           = "RDS_PASSWORD"
  value         = random_password.rds.result
  description   = "RDS master password — managed by Terraform"
}
```

#### Argument Reference

| Attribute        | Type   | Required | Description |
|-----------------|--------|----------|-------------|
| `provider_slug`  | string | ✅ | Slug of the provider (secret backend) within the environment |
| `key`            | string | ✅ | Secret name (e.g. `RDS_PASSWORD`). Changing this forces a new resource. |
| `value`          | string | ✅ | Secret value. **Sensitive** — hidden from logs and output. |
| `description`    | string | ❌ | Optional human-readable description |
| `project_slug`   | string | ❌ | Resolved from API key context. Override only if needed. |
| `environment_slug` | string | ❌ | Resolved from API key context. Override only if needed. |

#### Attribute Reference

| Attribute | Description |
|-----------|-------------|
| `id`      | Composite ID: `<project_slug>/<environment_slug>/<provider_slug>/<key>` |

#### Import

```bash
terraform import bella_secret.rds_password <project_slug>/<environment_slug>/<provider_slug>/<key>
```

> **Note:** After import, `value` will be empty in state. Run `terraform plan` — if there is a diff you may need to add `lifecycle { ignore_changes = [value] }` or set the correct value.

---

## Data Sources

### `data.bella_secret`

Reads a single secret value from Bella Baxter. All project and environment context is resolved automatically from the API key — you never need to specify `project_slug` or `environment_slug`.

```hcl
data "bella_secret" "db_password" {
  key = "RDS_PASSWORD"
}

output "db_pass" {
  value     = data.bella_secret.db_password.value
  sensitive = true
}
```

#### Argument Reference

| Attribute           | Required | Description |
|--------------------|----------|-------------|
| `key`              | ✅ | Secret name to look up |
| `project_slug`     | ❌ | Resolved from API key context |
| `environment_slug` | ❌ | Resolved from API key context |

#### Attribute Reference

| Attribute           | Description |
|--------------------|-------------|
| `id`               | `<project_slug>/<environment_slug>/<key>` |
| `value`            | The secret value (**sensitive**) |

---

### `data.bella_secrets`

Reads **all** secrets for an environment as a map. All project and environment context is resolved automatically from the API key.

```hcl
data "bella_secrets" "all" {}

# Access individual values
output "db_url" {
  value     = data.bella_secrets.all.secrets["DATABASE_URL"]
  sensitive = true
}
```

#### Argument Reference

| Attribute           | Required | Description |
|--------------------|----------|-------------|
| `project_slug`     | ❌ | Resolved from API key context |
| `environment_slug` | ❌ | Resolved from API key context |

#### Attribute Reference

| Attribute  | Description |
|-----------|-------------|
| `id`      | `<project_slug>/<environment_slug>` |
| `secrets` | `map(string)` of all secrets (**sensitive**) |
| `version` | Monotonically increasing version counter |

---

## Building from Source

```bash
git clone https://github.com/cosmic-chimps/terraform-provider-bella-baxter.git
cd terraform-provider-bella-baxter

# Option A — dev_overrides (recommended for local dev)
# Builds the binary and adds a dev_overrides block to ~/.terraformrc.
# After this, skip 'terraform init' and run 'terraform plan' directly.
make dev

# Option B — install into Terraform's plugin cache
# Builds the binary and copies it to ~/.terraform.d/plugins/...
# After this, run 'terraform init' as normal.
make install
```

---

## License

Apache 2.0 — see [LICENSE](../../../../LICENSE) for details.
