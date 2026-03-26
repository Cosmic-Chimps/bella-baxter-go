#!/bin/bash
set -euo pipefail
exec > >(tee /var/log/user-data.log | logger -t user-data -s 2>/dev/console) 2>&1

echo "=== [1/5] System update ==="
apt-get update -y
apt-get upgrade -y
apt-get install -y curl wget git unzip jq

echo "=== [2/5] Install Docker ==="
curl -fsSL https://get.docker.com | sh
usermod -aG docker ubuntu
systemctl enable docker
systemctl start docker

echo "=== [3/5] Install Docker Compose plugin ==="
mkdir -p /usr/local/lib/docker/cli-plugins
curl -SL "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64" \
  -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

echo "=== [4/5] Install Dokploy ==="
curl -sSL https://dokploy.com/install.sh | sh

echo "=== [5/5] Signal readiness ==="
# Write a ready file; the provisioner script will poll for it
touch /home/ubuntu/.dokploy_install_done
chown ubuntu:ubuntu /home/ubuntu/.dokploy_install_done

echo "=== Bootstrap complete. Dokploy is starting... ==="
