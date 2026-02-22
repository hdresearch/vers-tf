output "layer0_commit_id" {
  value       = vers_vm_commit.layer0.commit_id
  description = "Layer 0 (Base OS) commit ID"
}

output "layer1_commit_id" {
  value       = vers_vm_commit.layer1.commit_id
  description = "Layer 1 (Dev Tools) commit ID"
}

output "layer2_commit_id" {
  value       = vers_vm_commit.layer2.commit_id
  description = "Layer 2 (App) commit ID"
}

output "worker_vm_ids" {
  value       = vers_vm_restore.workers[*].id
  description = "Worker VM IDs (Layer 3)"
}

output "worker_ssh_hosts" {
  value       = vers_vm_restore.workers[*].ssh_host
  description = "Worker SSH hostnames"
}
