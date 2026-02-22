output "layer0_commit" {
  value = vers_vm_commit.layer0.commit_id
}

output "layer1_commit" {
  value = vers_vm_commit.layer1.commit_id
}

output "layer2_commit" {
  value = vers_vm_commit.layer2.commit_id
}

output "worker_ids" {
  value = vers_vm_restore.workers[*].id
}

output "worker_hosts" {
  value = vers_vm_restore.workers[*].ssh_host
}
