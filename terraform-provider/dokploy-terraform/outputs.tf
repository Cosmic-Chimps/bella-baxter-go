output "dokploy_ui_url" {
  description = "Dokploy web UI — open this in your browser"
  value       = "http://${aws_eip.dokploy.public_ip}:3000"
}

output "public_ip" {
  description = "EC2 elastic public IP"
  value       = aws_eip.dokploy.public_ip
}

output "ssh_command" {
  description = "SSH command to connect to the instance"
  value       = "ssh -i dokploy.pem ubuntu@${aws_eip.dokploy.public_ip}"
}

output "private_key_path" {
  description = "Path to the generated SSH private key"
  value       = local_sensitive_file.private_key.filename
}

output "instance_id" {
  description = "EC2 instance ID"
  value       = aws_instance.dokploy.id
}
