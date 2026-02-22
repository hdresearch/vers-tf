# ============================================================
# Layer 3: Workers
#
# No provisioning needed — just restore N VMs from L2 commit.
# Each worker starts with the full app environment ready to go.
# ============================================================

resource "vers_vm_restore" "workers" {
  count     = var.worker_count
  commit_id = vers_vm_commit.layer2.commit_id
}
