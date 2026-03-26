# Bella Baxter Terraform Provider — Examples

Two examples are available:

| Example | What it demonstrates |
|---------|----------------------|
| [`main.tf`](./main.tf) | Read a single secret, read all secrets, create/update a secret |
| [`ec2-with-bella-ssh/`](./ec2-with-bella-ssh/) | Provision an EC2 instance with certificate-based SSH via Bella's SSH CA |

The EC2 example has its own [README](./ec2-with-bella-ssh/README.md). This document covers the **basic `main.tf` example** step by step.

---

## Prerequisites

- [Terraform](https://developer.hashicorp.com/terraform/downloads) ≥ 1.5
- [Go](https://golang.org/dl/) ≥ 1.22
- A running Bella Baxter instance (local Aspire dev stack or hosted)
- A Bella Baxter project with at least one environment and one provider configured

---

## Step 1 — Build and install the provider locally

The provider is not yet published to the Terraform Registry. You need to build it and make it available to Terraform. There are two ways — pick one.

### Option A — `dev_overrides` (recommended for development)

This writes a **local** `.terraformrc` in the provider directory — it does **not** touch `~/.terraformrc`.

```bash
# From the repo root
cd apps/sdk/go/terraform-provider

# 1. Build the binary and write .terraformrc locally
make dev

# 2. Point Terraform at the local config for this shell session
export TF_CLI_CONFIG_FILE=$(pwd)/.terraformrc
# Add this line to ~/.zshrc or ~/.bashrc to persist across sessions
```

`make dev` generates `.terraformrc` in the provider directory:

```hcl
provider_installation {
  dev_overrides {
    "cosmic-chimps/bella-baxter" = "/abs/path/to/terraform-provider"   # directory, not binary
  }
  direct {}
}
```

`TF_CLI_CONFIG_FILE` tells Terraform which config to use. Without it Terraform falls back to `~/.terraformrc` (empty → registry lookup → fails).

---

### Option B — install into Terraform's plugin cache

This is the standard approach. Terraform finds the binary automatically during `terraform init`.

```bash
cd apps/sdk/go/terraform-provider
make install
```

This builds the binary and copies it to:
```
~/.terraform.d/plugins/registry.terraform.io/cosmic-chimps/bella-baxter/<version>/<os>_<arch>/terraform-provider-bella-baxter
```

---

## Step 2 — Get your API key

1. Open the Bella Baxter WebApp
2. Navigate to **Project → API Keys**
3. Create a new key — it will look like `bax-<id>-<secret>`
4. Note the **provider slug** (the name of your Vault / AWS / etc. provider inside Bella)

> The API key is already scoped to a specific project + environment.
> You never need to specify `project_slug` or `environment_slug` in your Terraform config.

---

## Step 3 — Set credentials

```bash
export BELLA_BAXTER_URL="https://baxter.example.com"   # or http://localhost:5001 for local dev
export BELLA_API_KEY="bax-your-key-id-your-signing-secret"
export TF_VAR_bella_api_key="$BELLA_API_KEY"
```

Or create a `terraform.tfvars` file in the `examples/` directory (already in `.gitignore`):

```hcl
bella_api_key = "bax-your-key-id-your-signing-secret"
```

---

## Step 4 — Customise `main.tf`

Open `main.tf` and replace the placeholder values:

| Placeholder | Replace with |
|-------------|-------------|
| `https://baxter.example.com` | Your Bella Baxter API URL |
| `my-vault` | Your provider slug (the backend name in Bella) |
| `RDS_PASSWORD`, `DATABASE_URL` | Any secret keys that actually exist in your environment |

> **Tip:** If you just want to test reads, comment out the `bella_secret` resource blocks
> and only run the `data` sources — they are read-only and safe to experiment with.

---

## Step 5 — Initialize Terraform

```bash
cd examples
terraform init
```

Terraform will install the hashicorp providers from the lock file. The `cosmic-chimps/bella-baxter` provider is loaded from your local binary via `dev_overrides` (no registry lookup needed).

---

## Step 6 — Preview changes

```bash
terraform plan
```

This shows what Terraform will read and write without making any changes.

---

## Step 7 — Apply

```bash
terraform apply
```

Type `yes` when prompted. Terraform will:

1. Read `bella_secret.db_password` (single secret lookup)
2. Read `bella_secrets.all_prod` (full environment secrets map)
3. Generate a random 32-char password via `random_password.rds`
4. Write `RDS_PASSWORD` → your Bella provider (`bella_secret.rds_password`)
5. Write `EXTERNAL_API_TOKEN` → your Bella provider (`bella_secret.external_api_token`)

---

## Step 8 — Inspect outputs

```bash
# Reveal sensitive output values
terraform output -raw db_url
terraform output -json
```

---

## Step 9 — Clean up

```bash
terraform destroy
```

---

## Troubleshooting

### `Error: Failed to query available provider packages`

The provider is not published to the public registry. You must build it locally — see **Step 1** above.
Make sure `TF_CLI_CONFIG_FILE` is exported and points to the `.terraformrc` in the provider directory.

### After deleting `.terraform/`

Just run `terraform init` again — the lock files are committed so it will reinstall the hashicorp providers
without touching the registry for `cosmic-chimps/bella-baxter`.

### `Error: 401 Unauthorized`

- Check that `BELLA_BAXTER_URL` points to a running instance
- Verify the API key is correct and has not been revoked

### `Error: provider slug not found`

The `provider_slug` in `bella_secret` must match the **name** of a provider assigned to that environment in the Bella WebApp (WebApp → Project → Environments → *your env* → Providers).

### `Error: Invalid index` on `secrets["DATABASE_URL"]`

The key `DATABASE_URL` does not exist in the environment. Replace it with a key that actually exists, or remove that output block.
