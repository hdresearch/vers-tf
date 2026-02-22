# ============================================================
# Layer 1: Dev Tools
#
# Restores from L0 commit, installs pi + swarm infra, commits.
# ============================================================

# Start a fresh VM from the Layer 0 snapshot
resource "vers_vm_restore" "layer1" {
  commit_id = vers_vm_commit.layer0.commit_id
}

resource "vers_provision" "layer1" {
  vm_id = vers_vm_restore.layer1.id

  files = [
    {
      source      = "${path.module}/scripts/tools.sh"
      destination = "/tmp/tools.sh"
    },
    # You can also inline content directly — useful for config files
    # that depend on Terraform variables:
    {
      content     = <<-EOF
        VERS_INFRA_URL=${var.infra_url}
        VERS_AUTH_TOKEN=${var.auth_token}
      EOF
      destination = "/etc/environment.d/vers.conf"
    },
  ]

  commands = [
    "chmod +x /tmp/tools.sh",
    "bash /tmp/tools.sh",
  ]

  triggers = {
    script    = filesha256("${path.module}/scripts/tools.sh")
    infra_url = var.infra_url
  }
}

resource "vers_vm_commit" "layer1" {
  vm_id       = vers_vm_restore.layer1.id
  keep_paused = true

  depends_on = [vers_provision.layer1]

  triggers = {
    provision = vers_provision.layer1.id
  }
}
