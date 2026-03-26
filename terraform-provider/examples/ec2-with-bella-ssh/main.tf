terraform {
  required_providers {
    bella = {
      source  = "cosmic-chimps/bella-baxter"
      version = "~> 0.1"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
    local = {
      source  = "hashicorp/local"
      version = "~> 2.5"
    }
    null = {
      source  = "hashicorp/null"
    }
  }
}

# ── Provider configuration ────────────────────────────────────────────────────

provider "bella" {
  baxter_url = var.bella_url
  api_key    = var.bella_api_key
}

provider "aws" {
  region = var.aws_region
}

# ── Variables ────────────────────────────────────────────────────────────────

variable "bella_url" {
  description = "Bella Baxter API URL (or set BELLA_BAXTER_URL env var)"
  type        = string
  default     = "https://baxter.example.com"
}

variable "bella_api_key" {
  description = "Bella Baxter API key (starts with 'bax-'). Obtain from WebApp → Project → API Keys."
  type        = string
  sensitive   = true
}

variable "aws_region" {
  description = "AWS region for the EC2 instance"
  type        = string
  default     = "us-east-1"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.micro"
}

variable "ami_id" {
  description = "AMI ID for the EC2 instance. Defaults to Ubuntu 24.04 LTS in us-east-1."
  type        = string
  default     = "ami-0c7217cdde317cfec" # Ubuntu 24.04 LTS, us-east-1
}

variable "ssh_allowed_users" {
  description = "Comma-separated Unix usernames that Bella SSH certificates will be valid for"
  type        = string
  default     = "ubuntu"
}

# ── Step 1: Bella SSH — declare the role ────────────────────────────────────
#
# bella_ssh_role creates a named role in Bella Baxter's SSH CA.
# The role controls which Unix usernames certificates may target and how
# long they stay valid. A single role can be shared by many team members.

resource "bella_ssh_role" "ops" {
  name          = "ops-team"
  allowed_users = var.ssh_allowed_users
  default_ttl   = "8h"
  max_ttl       = "24h"
}

# ── Step 2: Bella SSH — read the CA public key ───────────────────────────────
#
# bella_ssh_ca_public_key fetches the CA public key for this project/env.
# We write it to the EC2 instance via user_data so that sshd trusts
# any certificate signed by Bella's CA.

data "bella_ssh_ca_public_key" "this" {
  # CA key is always available once SSH is configured on the environment
  depends_on = [bella_ssh_role.ops]
}

# ── Step 3: Bella SSH — sign a Terraform-generated key for provisioning ──────
#
# During `terraform apply` we generate a temporary key pair so that
# Terraform's "connection" block can SSH in to run provisioners.
# The private key stays in Terraform state; the signed certificate is
# short-lived (1h) and scoped to the "ubuntu" user.
#
# In normal day-to-day use, operators sign THEIR OWN public key via:
#   bella ssh sign --project my-project --env production --role ops-team
# and then SSH directly — no Terraform involvement needed.

resource "tls_private_key" "terraform_provisioner" {
  algorithm = "ED25519"
}

data "bella_ssh_signed_certificate" "terraform_provisioner" {
  role_name        = bella_ssh_role.ops.name
  public_key       = tls_private_key.terraform_provisioner.public_key_openssh
  valid_principals = "ubuntu"
  ttl              = "1h"
}

# Write the signed certificate next to the private key so openssh can find it.
# The file name MUST be <private_key_file>-cert.pub for OpenSSH auto-detection.
resource "local_sensitive_file" "terraform_cert" {
  filename        = "${path.module}/.terraform-ssh-cert.pub"
  content         = data.bella_ssh_signed_certificate.terraform_provisioner.signed_key
  file_permission = "0600"
}

# Write the provisioner private key to disk so operators can SSH without needing
# their own key pair. Use: ssh -i .terraform-ssh-key ubuntu@<ip>
# (OpenSSH auto-loads .terraform-ssh-cert.pub because of the matching name)
resource "local_sensitive_file" "terraform_private_key" {
  filename        = "${path.module}/.terraform-ssh-key"
  content         = tls_private_key.terraform_provisioner.private_key_openssh
  file_permission = "0600"
}

# ── Step 4: AWS networking (minimal — single public subnet) ──────────────────

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true

  tags = { Name = "bella-ssh-example" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "bella-ssh-example" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  map_public_ip_on_launch = true

  tags = { Name = "bella-ssh-example-public" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = { Name = "bella-ssh-example-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# Allow SSH inbound only — no hardcoded key pairs needed.
resource "aws_security_group" "ssh" {
  name        = "bella-ssh-example"
  description = "Allow inbound SSH (CA-cert-only)"
  vpc_id      = aws_vpc.this.id

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "bella-ssh-example" }
}

# ── Step 5: EC2 instance — trust Bella's SSH CA via user_data ────────────────
#
# The cloud-init script:
#   1. Writes Bella's CA public key to /etc/ssh/bella_ca.pub
#   2. Adds TrustedUserCAKeys to sshd_config — any cert signed by Bella is accepted
#   3. Disables password auth and root login for good measure
#   4. Restarts sshd
#
# After this runs, ANY user whose public key has been signed by Bella's CA
# for the "ubuntu" principal can SSH in — no AWS key pair needed.

resource "aws_instance" "this" {
  ami                    = var.ami_id
  instance_type          = var.instance_type
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.ssh.id]

  user_data = <<-EOF
    #!/bin/bash
    set -euo pipefail

    # ── Trust Bella's SSH CA ──────────────────────────────────────────────────
    mkdir -p /etc/ssh
    cat > /etc/ssh/bella_ca.pub <<'CAKEY'
    ${data.bella_ssh_ca_public_key.this.ca_public_key}
    CAKEY

    # Configure sshd to trust Bella CA certificates
    cat >> /etc/ssh/sshd_config <<'SSHD'

    # Bella Baxter SSH CA — managed by Terraform
    TrustedUserCAKeys /etc/ssh/bella_ca.pub
    PasswordAuthentication no
    PermitRootLogin no
    SSHD

    systemctl restart sshd
    echo "Bella SSH CA configured ✓"
  EOF

  tags = {
    Name      = "bella-ssh-example"
    ManagedBy = "terraform"
  }
}

# ── Step 6 (optional): run a provisioner using the signed certificate ─────────
#
# This demonstrates Terraform connecting to the instance using a Bella-signed
# cert. In practice you would omit this block and let operators connect
# directly using `bella ssh sign` + `ssh -i ~/.ssh/id_ed25519`.

resource "null_resource" "verify_connection" {
  depends_on = [aws_instance.this, local_sensitive_file.terraform_cert]

  triggers = {
    instance_id = aws_instance.this.id
  }

  connection {
    type        = "ssh"
    host        = aws_instance.this.public_ip
    user        = "ubuntu"
    private_key = tls_private_key.terraform_provisioner.private_key_openssh
    certificate = data.bella_ssh_signed_certificate.terraform_provisioner.signed_key
    timeout     = "5m"
  }

  provisioner "remote-exec" {
    inline = [
      "echo 'Connected via Bella SSH certificate ✓'",
      "whoami",
      "hostname",
    ]
  }
}

# ── Outputs ───────────────────────────────────────────────────────────────────

output "instance_public_ip" {
  description = "Public IP of the EC2 instance"
  value       = aws_instance.this.public_ip
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.this.id
}

output "ssh_connect_command" {
  description = "How to SSH into the instance after signing your key with Bella"
  value       = "ssh ubuntu@${aws_instance.this.public_ip}"
}

output "bella_sign_command" {
  description = "Sign your personal SSH key with Bella before connecting"
  value       = "bella ssh sign --role ops-team"
}

output "quick_ssh_command" {
  description = "SSH using the Terraform-generated key (no personal key needed)"
  value       = "ssh -i ${path.module}/.terraform-ssh-key ubuntu@${aws_instance.this.public_ip}"
}

output "ca_public_key" {
  description = "Bella SSH CA public key (already installed on the instance)"
  value       = data.bella_ssh_ca_public_key.this.ca_public_key
}
