terraform {
  required_providers {
    vers = {
      source = "hdr/vers"
    }
  }
}

provider "vers" {
  # Uses VERS_API_KEY env var
}

# Create a basic VM
resource "vers_vm" "dev" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192
  wait_boot    = true
}

# Install some packages
resource "vers_provision" "setup" {
  vm_id = vers_vm.dev.id

  commands = [
    "export DEBIAN_FRONTEND=noninteractive",
    "apt-get update -qq",
    "apt-get install -y -qq git curl jq tree > /dev/null 2>&1",
  ]
}

output "vm_id" {
  value = vers_vm.dev.id
}

output "ssh_host" {
  value = vers_vm.dev.ssh_host
}
