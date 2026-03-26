# EC2 with Bella SSH — Example

This example shows how to provision an EC2 instance that uses **Bella Baxter's SSH CA** for certificate-based access. No static AWS key pairs are required.

## How it works

```
┌──────────────────────────────────────────────────────────┐
│  Terraform apply                                          │
│                                                           │
│  1. bella_ssh_role       → creates "ops-team" role       │
│  2. bella_ssh_ca_public_key → fetches CA public key      │
│  3. EC2 user_data        → writes CA key to sshd_config  │
│                             (TrustedUserCAKeys)           │
│  4. bella_ssh_signed_certificate → signs a temp key      │
│     for the provisioner connection block                  │
└──────────────────────────────────────────────────────────┘

Day-to-day access (no Terraform needed):
  bella ssh sign --project my-project --env production --role ops-team
  ssh ubuntu@<instance-ip>
```

Once the instance is running, any operator can SSH in by signing their own public key through Bella — no AWS key pairs, no bastion, no long-lived credentials.

## Prerequisites

- Bella Baxter account with a project and environment that has a Vault/OpenBao provider
- SSH configured on that environment (`ConfigureSsh` endpoint or the WebApp → Environments → SSH tab)
- AWS credentials (`aws configure` or `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` env vars)

## Usage

```bash
# 1. Copy example vars
cp terraform.tfvars.example terraform.tfvars

# 2. Edit terraform.tfvars with your values
#    bella_url, project_slug, bella_api_key

# 3. Apply
terraform init
terraform apply
```

## Connect after apply

After `terraform apply`, two files are written to this directory:

| File | What it is |
|------|-----------|
| `.terraform-ssh-cert.pub` | Signed certificate (used by the Terraform provisioner) |
| `.terraform-ssh-key` | Private key matching the cert above |

### Option A — Use the Terraform-generated key (quickest)

```bash
# The cert is already signed and on disk — just SSH directly:
ssh -i .terraform-ssh-key ubuntu@$(terraform output -raw instance_public_ip)
```

### Option B — Sign your own key with Bella (day-to-day workflow)

```bash
# 1. Make sure you have an SSH key pair (skip if you already have ~/.ssh/id_ed25519)
ssh-keygen -t ed25519 -C "your@email.com"

# 2. Sign your public key with Bella (run once per session, or when cert expires)
#    Bella auto-detects ~/.ssh/id_ed25519.pub, id_ecdsa.pub, or id_rsa.pub
bella ssh sign --role ops-team

#    If your key is in a non-standard location, pass it explicitly:
bella ssh sign --role ops-team --key ~/.ssh/my_key.pub

# 3. SSH in — OpenSSH picks up the signed cert automatically
ssh ubuntu@$(terraform output -raw instance_public_ip)
```

The signed certificate is written next to your public key as `id_ed25519-cert.pub`. OpenSSH loads it automatically — no `-i` flags needed if your key is in `~/.ssh/`.

## What's created

| Resource | Description |
|----------|-------------|
| `bella_ssh_role.ops` | SSH role "ops-team" scoped to this project/env |
| `bella_ssh_ca_public_key` | Fetches CA key to trust on the instance |
| `bella_ssh_signed_certificate` | Short-lived cert for Terraform's provisioner |
| `aws_vpc` + `aws_subnet` + `aws_internet_gateway` | Minimal networking |
| `aws_security_group.ssh` | Inbound SSH (port 22) only |
| `aws_instance.this` | Ubuntu 24.04 EC2 with Bella CA in `sshd_config` |
| `null_resource.verify_connection` | Runs `whoami` / `hostname` to verify cert auth works |

## Cleanup

```bash
terraform destroy
```
