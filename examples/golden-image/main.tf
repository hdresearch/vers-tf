terraform {
  required_providers {
    vers = {
      source = "hdr/vers"
    }
  }
}

provider "vers" {
  api_key = var.vers_api_key != "" ? var.vers_api_key : null
}

# --- Layer 0: Base VM ---

resource "vers_vm" "golden_base" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192
  wait_boot    = true
}

# --- Provision the golden image ---

resource "vers_provision" "golden_setup" {
  vm_id = vers_vm.golden_base.id

  files = [
    {
      source      = "${path.module}/scripts/bootstrap.sh"
      destination = "/tmp/bootstrap.sh"
    },
  ]

  commands = [
    "chmod +x /tmp/bootstrap.sh",
    "bash /tmp/bootstrap.sh",
    "mkdir -p /root/.swarm/status",
    "echo '{\"vms\":[]}' > /root/.swarm/registry.json",
    "touch /root/.swarm/registry.lock",
  ]

  triggers = {
    bootstrap = filesha256("${path.module}/scripts/bootstrap.sh")
  }
}

# --- Commit as golden image ---

resource "vers_vm_commit" "golden" {
  vm_id       = vers_vm.golden_base.id
  keep_paused = true

  depends_on = [vers_provision.golden_setup]

  triggers = {
    provision_id = vers_provision.golden_setup.id
  }
}

# --- Optional: spawn workers from golden image ---

resource "vers_vm_restore" "worker" {
  count     = var.worker_count
  commit_id = vers_vm_commit.golden.commit_id
}
