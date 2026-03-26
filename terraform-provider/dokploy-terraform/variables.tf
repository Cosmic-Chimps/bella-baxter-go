variable "aws_region" {
  description = "AWS region to deploy into"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Prefix used for all resource names"
  type        = string
  default     = "dokploy"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.medium"
}

variable "root_volume_size_gb" {
  description = "Root EBS volume size in GB"
  type        = number
  default     = 30
}

variable "allowed_ssh_cidr" {
  description = "CIDR blocks allowed to SSH. Restrict this to your IP for security."
  type        = list(string)
  default     = ["0.0.0.0/0"] # ⚠️  Change to your IP: ["x.x.x.x/32"]
}

variable "dokploy_admin_email" {
  description = "Email address for the Dokploy admin account"
  type        = string
}

variable "dokploy_admin_password" {
  description = "Password for the Dokploy admin account"
  type        = string
  sensitive   = true
}

variable "github_token" {
  description = "GitHub personal access token (required for private repos, optional for public)"
  type        = string
  sensitive   = true
  default     = ""
}

variable "docker_compose_apps" {
  description = "List of Docker Compose apps to register in Dokploy"
  type = list(object({
    name        = string   # App name shown in Dokploy UI
    repo_url    = string   # Full GitHub HTTPS URL
    branch      = string   # Branch to deploy (e.g. "main")
    compose_file = string  # Path to docker-compose file inside the repo
    description = string   # Optional human-readable description
  }))
  default = []
}
