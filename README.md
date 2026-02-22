# terraform-provider-vers

Terraform provider for [Vers](https://vers.sh) — declaratively create, provision, snapshot, and manage Firecracker micro-VMs.

## Quick Start

```hcl
terraform {
  required_providers {
    vers = {
      source = "hdr/vers"
    }
  }
}

provider "vers" {
  # Uses VERS_API_KEY env var by default
}

# Create a VM
resource "vers_vm" "app" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192
  wait_boot    = true
}

# Provision it
resource "vers_provision" "setup" {
  vm_id = vers_vm.app.id

  files = [
    {
      source      = "scripts/bootstrap.sh"
      destination = "/tmp/bootstrap.sh"
    },
  ]

  commands = [
    "chmod +x /tmp/bootstrap.sh",
    "bash /tmp/bootstrap.sh",
  ]

  triggers = {
    script = filesha256("scripts/bootstrap.sh")
  }
}

# Snapshot as a golden image
resource "vers_vm_commit" "golden" {
  vm_id       = vers_vm.app.id
  keep_paused = true
  depends_on  = [vers_provision.setup]

  triggers = {
    provision = vers_provision.setup.id
  }
}

# Spawn workers from the golden image
resource "vers_vm_restore" "worker" {
  count     = 3
  commit_id = vers_vm_commit.golden.commit_id
}
```

## Layered VMs

The real power is **layered VM snapshots** — each layer builds on the last, like Docker layers but for full VMs. When a layer changes, all downstream layers are rebuilt:

```hcl
# Layer 0: Base OS
resource "vers_vm" "base" { ... }
resource "vers_provision" "base" { vm_id = vers_vm.base.id ... }
resource "vers_vm_commit" "base" { vm_id = vers_vm.base.id }

# Layer 1: Dev tools (from Layer 0 snapshot)
resource "vers_vm_restore" "dev" { commit_id = vers_vm_commit.base.commit_id }
resource "vers_provision" "dev" { vm_id = vers_vm_restore.dev.id ... }
resource "vers_vm_commit" "dev" { vm_id = vers_vm_restore.dev.id }

# Layer 2: App (from Layer 1 snapshot)
resource "vers_vm_restore" "app" { commit_id = vers_vm_commit.dev.commit_id }
resource "vers_provision" "app" { vm_id = vers_vm_restore.app.id ... }
resource "vers_vm_commit" "app" { vm_id = vers_vm_restore.app.id }

# Layer 3: Workers (from Layer 2 snapshot)
resource "vers_vm_restore" "workers" {
  count     = var.worker_count
  commit_id = vers_vm_commit.app.commit_id
}
```

See [`examples/layered-vms/`](examples/layered-vms/) for a complete 4-layer example.

## Resources

### `vers_vm`

Creates a root Firecracker VM.

| Attribute | Type | Required | Description |
|---|---|---|---|
| `vcpu_count` | number | optional | vCPUs (default: 1) |
| `mem_size_mib` | number | optional | RAM in MiB (default: 2048) |
| `fs_size_mib` | number | optional | Disk in MiB (default: 4096) |
| `wait_boot` | bool | optional | Wait for boot (default: true) |

**Computed:** `id`, `state`, `ssh_host`, `ssh_private_key` (sensitive), `created_at`

### `vers_vm_commit`

Snapshot a VM to a reusable commit.

| Attribute | Type | Required | Description |
|---|---|---|---|
| `vm_id` | string | **required** | VM to snapshot |
| `keep_paused` | bool | optional | Pause source VM after commit (default: false) |
| `triggers` | map(string) | optional | Trigger re-commit when values change |

**Computed:** `commit_id`

### `vers_vm_restore`

Create a new VM from a commit snapshot.

| Attribute | Type | Required | Description |
|---|---|---|---|
| `commit_id` | string | **required** | Commit to restore from |

**Computed:** `id`, `state`, `ssh_host`, `ssh_private_key` (sensitive), `created_at`

### `vers_vm_branch`

Clone a running VM via copy-on-write.

| Attribute | Type | Required | Description |
|---|---|---|---|
| `source_vm_id` | string | **required** | VM to clone |

**Computed:** `id`, `state`, `ssh_host`, `ssh_private_key` (sensitive), `created_at`

### `vers_provision`

Upload files and run commands on a VM via SSH-over-TLS.

| Attribute | Type | Required | Description |
|---|---|---|---|
| `vm_id` | string | **required** | VM to provision |
| `files` | list(object) | optional | Files to upload (see below) |
| `commands` | list(string) | optional | Shell commands to run (in order, after files) |
| `triggers` | map(string) | optional | Trigger re-provision when values change |

**File object:**

| Field | Description |
|---|---|
| `source` | Local file path to upload (mutually exclusive with `content`) |
| `content` | Inline string content (supports `templatefile()`) |
| `destination` | Remote path on the VM |

## Data Sources

### `vers_vms`

List all VMs.

```hcl
data "vers_vms" "all" {}
# => data.vers_vms.all.vms[*].id
```

## Authentication

Set `VERS_API_KEY` as an environment variable:

```bash
export VERS_API_KEY="your-key-here"
terraform plan
```

Or in the provider block:

```hcl
provider "vers" {
  api_key = var.vers_api_key
}
```

## How Provisioning Works

Vers VMs are reachable via SSH tunneled through TLS. Standard Terraform provisioners don't support this transport. The `vers_provision` resource handles it automatically using `openssl s_client` as a ProxyCommand — the same mechanism used by the [pi Vers extension](https://github.com/hdr-is/pi-v).

The provisioning flow:
1. Fetch SSH credentials via Vers API (`GET /vm/{id}/ssh_key`)
2. Write private key to a temp file
3. Wait for VM to be reachable via SSH-over-TLS
4. Upload files via SSH stdin pipe (base64 encoded for safety)
5. Execute commands sequentially via SSH
6. Clean up temp key file

## Requirements

- Terraform >= 1.0
- `ssh` and `openssl` on PATH (for VM provisioning)
- A Vers API key

## Development

```bash
# Build
go build -o terraform-provider-vers

# Dev override (use local build instead of registry)
cat >> ~/.terraformrc << 'EOF'
provider_installation {
  dev_overrides {
    "hdr/vers" = "/path/to/vers-tf"
  }
  direct {}
}
EOF

# Test
cd examples/basic-vm
export VERS_API_KEY="your-key"
terraform plan
terraform apply
terraform destroy
```

## Examples

| Example | Description |
|---|---|
| [`basic-vm`](examples/basic-vm/) | Create and provision a single VM |
| [`golden-image`](examples/golden-image/) | Build a golden image and spawn workers |
| [`layered-vms`](examples/layered-vms/) | 4-layer VM pipeline with cascading rebuilds |

## License

MIT
