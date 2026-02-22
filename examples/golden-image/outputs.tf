output "golden_vm_id" {
  value       = vers_vm.golden_base.id
  description = "The base VM ID (paused after commit)"
}

output "golden_commit_id" {
  value       = vers_vm_commit.golden.commit_id
  description = "Golden image commit ID — use this to spawn VMs"
}

output "worker_vm_ids" {
  value       = vers_vm_restore.worker[*].id
  description = "Worker VM IDs restored from the golden image"
}

output "worker_ssh_hosts" {
  value       = vers_vm_restore.worker[*].ssh_host
  description = "Worker SSH hostnames"
}
