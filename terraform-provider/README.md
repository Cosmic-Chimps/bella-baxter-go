# Bella Baxter Terraform Provider

A Terraform provider for [Bella Baxter](https://github.com/cosmic-chimps/bella-baxter) — a secret management gateway that proxies CRUD operations to external secret backends (HashiCorp Vault / OpenBao, AWS Secrets Manager, Azure Key Vault, GCP Secret Manager).

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) ≥ 1.5
- [Go](https://golang.org/) ≥ 1.22 (to build from source)
- A running Bella Baxter API instance

---

## Provider Configuration

```hcl
terraform {
  required_providers {
    bella = {
      source  = "cosmic-chimps/bella-baxter"
      # Pre-release — pin to exact version until a stable 0.1.x is published.
      # Terraform's ~> operator does not match pre-release versions.
      version = "= 0.1.1-preview.74"
    }
  }
}

provider "bella" {
  baxter_url = "https://api.bella-baxter.io"  # or BELLA_BAXTER_URL env var
  api_key    = var.bella_api_key               # or BELLA_API_KEY env var (sensitive)
}
```

### Environment Variables

| Variable           | Description                                      |
|--------------------|--------------------------------------------------|
| `BELLA_BAXTER_URL` | Base URL of the Bella Baxter API                 |
| `BELLA_API_KEY`    | API key (starts with `bax-`, sensitive)          |

> **Note:** The API key already encodes which project and environment it is scoped to.
> `project_slug` and `environment_slug` are resolved automatically from the key via `GET /api/v1/keys/me` — you never need to specify them explicitly.

### In CI (GitHub Actions with OIDC)

Use the [`bella-baxter-setup-action`](https://github.com/Cosmic-Chimps/bella-baxter-setup-action) and `bella auth oidc` to exchange a GitHub OIDC token for a short-lived `BELLA_API_KEY` — no long-lived secrets needed in GitHub Actions.

```yaml
- uses: Cosmic-Chimps/bella-baxter-setup-action@main

- name: Authenticate with Bella
  run: bella auth oidc
  env:
    BELLA_BAXTER_URL: https://api.bella-baxter.io
    BELLA_BAXTER_TENANT: my-org
    BELLA_BAXTER_PROJECT: my-project
    BELLA_BAXTER_ENV: production

- name: Terraform Init & Plan
  run: bella run -- sh -c 'terraform init && terraform plan'
  # bella run injects all Bella secrets as env vars before running the command
```

---

## Resources

### `bella_secret`

Manages a single secret within a Bella Baxter environment. The secret is stored in the external provider (Vault, AWS, etc.) — **not** in Terraform state.

Project and environment context are resolved automatically from the API key.

```hcl
resource "random_password" "rds" {
  length  = 32
  special = false
}

resource "bella_secret" "rds_password" {
  provider_slug = "my-vault"   # backend slug — see WebApp → Environment → Providers
  key           = "RDS_PASSWORD"
  value         = random_password.rds.result
  description   = "RDS master password — managed by Terraform"
}

resource "bella_secret" "database_url" {
  provider_slug = "my-vault"
  key           = "DATABASE_URL"
  value         = "postgres://app:${random_password.rds.result}@${aws_db_instance.main.address}/app"
  description   = "Full connection string for the application"
  depends_on    = [aws_db_instance.main]
}
```

#### Argument Reference

| Attribute           | Type   | Required | Description |
|---------------------|--------|----------|-------------|
| `provider_slug`     | string | ✅       | Slug of the provider (secret backend) within the environment |
| `key`               | string | ✅       | Secret name (e.g. `RDS_PASSWORD`). Changing this forces a new resource. |
| `value`             | string | ✅       | Secret value. **Sensitive** — hidden from logs and plan output. |
| `description`       | string | ❌       | Optional human-readable description |
| `project_slug`      | string | ❌       | Resolved from API key context. Override only if needed. |
| `environment_slug`  | string | ❌       | Resolved from API key context. Override only if needed. |

#### Attribute Reference

| Attribute | Description |
|-----------|-------------|
| `id`      | Composite ID: `<project_slug>/<environment_slug>/<provider_slug>/<key>` |

#### Import

```bash
terraform import bella_secret.rds_password <project_slug>/<environment_slug>/<provider_slug>/<key>
```

> After import, `value` will be empty in state. Add `lifecycle { ignore_changes = [value] }` to prevent spurious diffs, or re-set the correct value.

---

### `bella_ssh_role`

Manages an SSH role in a Bella Baxter environment. SSH roles define which Unix usernames and certificate TTLs are allowed when signing public keys via Bella's SSH CA.

```hcl
resource "bella_ssh_role" "ops" {
  name          = "ops-team"
  allowed_users = "ubuntu,ec2-user,admin"
  default_ttl   = "8h"
  max_ttl       = "24h"
}
```

#### Argument Reference

| Attribute       | Type   | Required | Description |
|-----------------|--------|----------|-------------|
| `name`          | string | ✅       | Role name (e.g. `ops-team`). Changing this forces a new resource. |
| `allowed_users` | string | ✅       | Comma-separated list of allowed Unix usernames. |
| `default_ttl`   | string | ❌       | Default certificate TTL (e.g. `8h`). Defaults to `8h`. |
| `max_ttl`       | string | ❌       | Maximum certificate TTL (e.g. `24h`). Defaults to `24h`. |
| `project_slug`  | string | ❌       | Resolved from API key context. |
| `environment_slug` | string | ❌    | Resolved from API key context. |

#### Attribute Reference

| Attribute | Description |
|-----------|-------------|
| `id`      | `<project_slug>/<environment_slug>/<name>` |

#### Import

```bash
terraform import bella_ssh_role.ops <project_slug>/<environment_slug>/<role_name>
```

---

### `bella_pki_role`

Manages a PKI role in a Bella Baxter environment. PKI roles define which Common Names and certificate TTLs are allowed when issuing TLS certificates via Bella's PKI engine.

```hcl
resource "bella_pki_role" "internal" {
  name            = "internal-service"
  allowed_domains = "internal.example.com"
  allow_subdomains = true
  default_ttl     = "720h"   # 30 days
  max_ttl         = "8760h"  # 1 year
  key_type        = "rsa"
}
```

#### Argument Reference

| Attribute         | Type   | Required | Description |
|-------------------|--------|----------|-------------|
| `name`            | string | ✅       | Role name (e.g. `internal-service`). Changing this forces a new resource. |
| `allowed_domains` | string | ✅       | Base domain(s) certificates may be issued for. Do **not** include wildcards — use `allow_subdomains = true` instead. |
| `allow_subdomains`| bool   | ❌       | Allow subdomains of `allowed_domains`. Defaults to `true`. |
| `allow_localhost` | bool   | ❌       | Allow `localhost` as CN or SAN. Defaults to `false`. |
| `allow_any_name`  | bool   | ❌       | Allow any CN regardless of `allowed_domains`. Defaults to `false`. |
| `default_ttl`     | string | ❌       | Default certificate TTL (e.g. `720h`). Defaults to `720h`. |
| `max_ttl`         | string | ❌       | Maximum certificate TTL (e.g. `8760h`). Defaults to `8760h`. |
| `key_type`        | string | ❌       | Key algorithm: `rsa` or `ec`. Defaults to `rsa`. |
| `project_slug`    | string | ❌       | Resolved from API key context. |
| `environment_slug`| string | ❌       | Resolved from API key context. |

#### Attribute Reference

| Attribute | Description |
|-----------|-------------|
| `id`      | `<project_slug>/<environment_slug>/<name>` |

#### Import

```bash
terraform import bella_pki_role.internal <project_slug>/<environment_slug>/<role_name>
```

---

## Data Sources

### `data.bella_secret`

Reads a single secret value from Bella Baxter.

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
|---------------------|----------|-------------|
| `key`               | ✅       | Secret name to look up |
| `project_slug`      | ❌       | Resolved from API key context |
| `environment_slug`  | ❌       | Resolved from API key context |

#### Attribute Reference

| Attribute | Description |
|-----------|-------------|
| `id`      | `<project_slug>/<environment_slug>/<key>` |
| `value`   | The secret value (**sensitive**) |

---

### `data.bella_secrets`

Reads **all** secrets for an environment as a map. Useful for feeding secrets into a `templatefile` or iterating over all keys.

```hcl
data "bella_secrets" "all" {}

# Access by name
output "db_url" {
  value     = data.bella_secrets.all.secrets["DATABASE_URL"]
  sensitive = true
}

# Write all secrets to a .env file for a local container
resource "local_file" "dotenv" {
  content  = join("\n", [for k, v in data.bella_secrets.all.secrets : "${k}=${v}"])
  filename = "${path.module}/.env"
}
```

#### Argument Reference

| Attribute           | Required | Description |
|---------------------|----------|-------------|
| `project_slug`      | ❌       | Resolved from API key context |
| `environment_slug`  | ❌       | Resolved from API key context |

#### Attribute Reference

| Attribute | Description |
|-----------|-------------|
| `id`      | `<project_slug>/<environment_slug>` |
| `secrets` | `map(string)` of all secrets (**sensitive**) |
| `version` | Monotonically increasing version counter |

---

### `data.bella_ssh_ca_public_key`

Reads the SSH CA public key for the environment. Use this to configure `TrustedUserCAKeys` on your SSH servers so that certificates signed by Bella are accepted.

```hcl
data "bella_ssh_ca_public_key" "this" {}

# Write the CA public key to a remote host via remote-exec
resource "aws_instance" "bastion" { /* ... */ }

resource "null_resource" "trust_bella_ca" {
  connection {
    type        = "ssh"
    host        = aws_instance.bastion.public_ip
    user        = "ubuntu"
    private_key = file("~/.ssh/id_ed25519")
  }

  provisioner "remote-exec" {
    inline = [
      "echo '${data.bella_ssh_ca_public_key.this.ca_public_key}' | sudo tee /etc/ssh/bella_ca.pub",
      "echo 'TrustedUserCAKeys /etc/ssh/bella_ca.pub' | sudo tee -a /etc/ssh/sshd_config",
      "sudo systemctl reload sshd",
    ]
  }
}
```

#### Argument Reference

| Attribute           | Required | Description |
|---------------------|----------|-------------|
| `project_slug`      | ❌       | Resolved from API key context |
| `environment_slug`  | ❌       | Resolved from API key context |

#### Attribute Reference

| Attribute        | Description |
|------------------|-------------|
| `id`             | `<project_slug>/<environment_slug>` |
| `ca_public_key`  | OpenSSH CA public key. Add as `TrustedUserCAKeys` on SSH servers. |
| `instructions`   | Human-readable setup instructions |
| `terraform_snippet` | Ready-to-use HCL snippet for writing the CA key to a remote host |

---

### `data.bella_ssh_signed_certificate`

Signs an SSH public key via Bella Baxter and returns a short-lived certificate. Pass `signed_key` as `connection.certificate` in a Terraform provisioner to connect to CA-trusted hosts without static key pairs.

```hcl
resource "bella_ssh_role" "ops" {
  name          = "ops-team"
  allowed_users = "ubuntu,ec2-user"
  default_ttl   = "1h"
}

data "bella_ssh_signed_certificate" "provisioner" {
  role_name  = bella_ssh_role.ops.name
  public_key = file("~/.ssh/id_ed25519.pub")
  principals = "ubuntu"
  ttl        = "1h"

  depends_on = [bella_ssh_role.ops]
}

resource "aws_instance" "web" { /* ... */ }

resource "null_resource" "configure" {
  connection {
    type        = "ssh"
    host        = aws_instance.web.public_ip
    user        = "ubuntu"
    private_key = file("~/.ssh/id_ed25519")
    certificate = data.bella_ssh_signed_certificate.provisioner.signed_key
  }

  provisioner "remote-exec" {
    inline = ["echo 'Provisioned via Bella SSH CA'"]
  }
}
```

#### Argument Reference

| Attribute    | Required | Description |
|--------------|----------|-------------|
| `role_name`  | ✅       | SSH role that governs allowed principals and TTLs |
| `public_key` | ✅       | OpenSSH public key to sign (e.g. contents of `~/.ssh/id_ed25519.pub`) |
| `principals` | ❌       | Comma-separated Unix usernames. Defaults to the role's `allowed_users`. |
| `ttl`        | ❌       | Certificate lifetime (e.g. `1h`). Defaults to the role's `default_ttl`. |
| `project_slug`     | ❌ | Resolved from API key context |
| `environment_slug` | ❌ | Resolved from API key context |

#### Attribute Reference

| Attribute      | Description |
|----------------|-------------|
| `id`           | Certificate serial number |
| `signed_key`   | The signed SSH certificate. Pass to `connection.certificate`. |
| `serial`       | Certificate serial number |
| `expires_at`   | Certificate expiry timestamp (RFC3339) |

---

### `data.bella_pki_ca`

Reads the PKI CA certificate for the environment. Distribute this certificate to clients and services that need to trust Bella-issued TLS certificates.

```hcl
data "bella_pki_ca" "this" {}

# Write the CA cert to a local file for distribution
resource "local_file" "ca_cert" {
  content  = data.bella_pki_ca.this.ca_cert_pem
  filename = "${path.module}/bella-ca.pem"
}

output "acme_directory_url" {
  description = "ACME directory URL for automatic cert issuance (Caddy, cert-manager, acme.sh)"
  value       = data.bella_pki_ca.this.acme_directory_url
}
```

#### Argument Reference

| Attribute           | Required | Description |
|---------------------|----------|-------------|
| `project_slug`      | ❌       | Resolved from API key context |
| `environment_slug`  | ❌       | Resolved from API key context |

#### Attribute Reference

| Attribute            | Description |
|----------------------|-------------|
| `id`                 | `<project_slug>/<environment_slug>` |
| `ca_cert_pem`        | CA certificate in PEM format |
| `ca_chain_pem`       | Full CA chain in PEM format (for intermediate CA setups) |
| `instructions`       | Human-readable distribution instructions |
| `acme_directory_url` | ACME directory URL (RFC 8555) — pass to certbot, acme.sh, Caddy, or cert-manager |

---

### `data.bella_pki_certificate`

Issues a short-lived X.509 TLS certificate from Bella Baxter's PKI engine. The private key is available only at issuance time — save it immediately.

```hcl
resource "bella_pki_role" "internal" {
  name            = "internal-service"
  allowed_domains = "internal.example.com"
  allow_subdomains = true
  default_ttl     = "720h"
}

data "bella_pki_certificate" "api" {
  role_name   = bella_pki_role.internal.name
  common_name = "api.internal.example.com"
  alt_names   = "api-v2.internal.example.com"
  ttl         = "720h"

  depends_on = [bella_pki_role.internal]
}

# Store the certificate and key as Bella secrets for the application to consume
resource "bella_secret" "tls_cert" {
  provider_slug = "my-vault"
  key           = "TLS_CERT"
  value         = data.bella_pki_certificate.api.certificate_pem
}

resource "bella_secret" "tls_key" {
  provider_slug = "my-vault"
  key           = "TLS_KEY"
  value         = data.bella_pki_certificate.api.private_key_pem
}
```

#### Argument Reference

| Attribute           | Required | Description |
|---------------------|----------|-------------|
| `role_name`         | ✅       | PKI role that governs which CNs and TTLs are allowed |
| `common_name`       | ✅       | Certificate CN (e.g. `api.internal.example.com`) |
| `alt_names`         | ❌       | Comma-separated SANs (e.g. `api-v2.internal.example.com`) |
| `ip_sans`           | ❌       | Comma-separated IP SANs (e.g. `10.0.0.5,192.168.1.1`) |
| `ttl`               | ❌       | Certificate lifetime (e.g. `24h`). Defaults to the role's `default_ttl`. |
| `project_slug`      | ❌       | Resolved from API key context |
| `environment_slug`  | ❌       | Resolved from API key context |

#### Attribute Reference

| Attribute           | Description |
|---------------------|-------------|
| `id`                | Certificate serial number |
| `certificate_pem`   | Issued certificate in PEM format |
| `private_key_pem`   | Private key in PEM format. **Sensitive** — available only at issuance time. |
| `key_type`          | Key algorithm (e.g. `rsa`) |
| `issuing_ca`        | Issuing CA certificate in PEM format |
| `ca_chain_pem`      | Full CA chain in PEM format |
| `serial`            | Certificate serial number (colon-delimited hex) |

---

## Complete Example: EC2 with Bella-managed Secrets + SSH CA

This example provisions an EC2 instance that:
- Has its SSH server configured to trust Bella's SSH CA
- Pulls application secrets at runtime via `bella run`
- Stores generated secrets (DB password, connection string) back into Bella

```hcl
terraform {
  required_providers {
    aws   = { source = "hashicorp/aws",            version = "~> 5.0" }
    bella = { source = "cosmic-chimps/bella-baxter", version = "= 0.1.1-preview.74" }
    random = { source = "hashicorp/random",         version = "~> 3.6" }
    tls   = { source = "hashicorp/tls",             version = "~> 4.0" }
  }
}

provider "aws" { region = var.aws_region }

provider "bella" {
  baxter_url = var.bella_baxter_url
  api_key    = var.bella_app_api_key
}

# ── SSH CA setup ───────────────────────────────────────────────────────────────

resource "bella_ssh_role" "ops" {
  name          = "ops-team"
  allowed_users = "ubuntu"
  default_ttl   = "8h"
  max_ttl       = "24h"
}

data "bella_ssh_ca_public_key" "this" {}

data "bella_ssh_signed_certificate" "provisioner" {
  role_name  = bella_ssh_role.ops.name
  public_key = tls_private_key.provisioner.public_key_openssh
  principals = "ubuntu"
  ttl        = "1h"
  depends_on = [bella_ssh_role.ops]
}

resource "tls_private_key" "provisioner" { algorithm = "ED25519" }

# ── Application secrets ────────────────────────────────────────────────────────

resource "random_password" "db" {
  length  = 32
  special = false
}

resource "bella_secret" "rds_password" {
  provider_slug = var.bella_provider_slug
  key           = "RDS_PASSWORD"
  value         = random_password.db.result
}

resource "bella_secret" "database_url" {
  provider_slug = var.bella_provider_slug
  key           = "DATABASE_URL"
  value         = "postgres://app:${random_password.db.result}@${aws_db_instance.main.address}/app"
  depends_on    = [aws_db_instance.main]
}

# ── EC2 instance ───────────────────────────────────────────────────────────────

resource "aws_instance" "app" {
  ami           = data.aws_ami.ubuntu.id
  instance_type = "t3.micro"

  connection {
    type        = "ssh"
    host        = self.public_ip
    user        = "ubuntu"
    private_key = tls_private_key.provisioner.private_key_pem
    certificate = data.bella_ssh_signed_certificate.provisioner.signed_key
  }

  provisioner "remote-exec" {
    inline = [
      # Trust the Bella SSH CA
      "echo '${data.bella_ssh_ca_public_key.this.ca_public_key}' | sudo tee /etc/ssh/bella_ca.pub",
      "echo 'TrustedUserCAKeys /etc/ssh/bella_ca.pub' | sudo tee -a /etc/ssh/sshd_config",
      "sudo systemctl reload sshd",
    ]
  }

  depends_on = [bella_secret.rds_password, bella_secret.database_url]
}
```

See the full working demo in [`apps/demos/look-ma-no-secrets/`](../../demos/look-ma-no-secrets/).

---

## Building from Source

```bash
git clone https://github.com/cosmic-chimps/terraform-provider-bella-baxter.git
cd terraform-provider-bella-baxter

# Option A — dev_overrides (recommended for local dev)
# Builds the binary and writes .terraformrc in the provider directory.
# Skip 'terraform init' and run 'terraform plan' directly.
export TF_CLI_CONFIG_FILE=$(pwd)/.terraformrc
make dev

# Option B — install into Terraform's plugin cache
# After this, run 'terraform init' as normal.
make install
```

---

## License

Apache 2.0 — see [LICENSE](../../../../LICENSE) for details.
