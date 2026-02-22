# ============================================================
# Layer 0: Base OS
#
# This is the only layer that creates a fresh VM.
# Every other layer restores from the previous layer's commit.
# ============================================================

resource "vers_vm" "layer0" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192
  wait_boot    = true
}

resource "vers_provision" "layer0" {
  vm_id = vers_vm.layer0.id

  # Upload your bootstrap script to the VM
  files = [
    {
      source      = "${path.module}/scripts/base.sh"
      destination = "/tmp/base.sh"
    },
  ]

  # Run it
  commands = [
    "chmod +x /tmp/base.sh",
    "bash /tmp/base.sh",
  ]

  # When the script changes, this layer re-provisions.
  # That cascades: layers 1, 2, 3 all rebuild too.
  triggers = {
    script = filesha256("${path.module}/scripts/base.sh")
  }
}

resource "vers_vm_commit" "layer0" {
  vm_id       = vers_vm.layer0.id
  keep_paused = true  # Don't need L0 VM running after snapshotting

  depends_on = [vers_provision.layer0]

  triggers = {
    provision = vers_provision.layer0.id
  }
}
