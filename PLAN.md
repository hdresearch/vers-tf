# vers-tf: Terraform Provider for Vers

## Vision

A custom Terraform provider that lets users declaratively define Vers VMs — their size, installed packages, files, services, and relationships — using standard HCL configuration. `terraform apply` produces running, reproducible VMs (or committed golden images) on the Vers platform.

This replaces the manual/scripted golden-image workflow with infrastructure-as-code.

---

## 1. Why Terraform for Vers?

**Current workflow** (manual/agent-driven):
1. `vers_vm_create` → boot a blank Ubuntu 24.04 VM
2. `vers_vm_use` → SSH in
3. Run bootstrap.sh (apt-get, Node.js, git, pi, etc.)
4. Copy extensions, context files, config
5. `vers_vm_commit` → snapshot as golden image
6. Share the commit ID manually

**Problems:**
- Not reproducible — bootstrap scripts drift, manual steps get forgotten
- No version control of VM definitions
- No dependency graph between VMs (golden → worker swarm)
- No plan/diff — you can't preview what will change
- No state tracking — lose track of which VMs came from what config

**Terraform solves all of this.** A `.tf` file IS the VM definition. `terraform plan` shows what will change. `terraform apply` makes it so. State is tracked. Commits are recorded. Teams can review VM configs in PRs.

---

## 2. Resource Model

### 2.1 `vers_vm` — Core VM Resource

```hcl
resource "vers_vm" "dev" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192

  # Optional: restore from an existing commit instead of creating from scratch
  # from_commit = "commit-abc123"

  # Wait for the VM to be ready before running provisioners
  wait_boot = true
}
```

**Lifecycle:**
- `Create` → calls `POST /vm/new_root` (or `/vm/from_commit` if `from_commit` set)
- `Read` → calls `GET /vms`, finds by ID
- `Update` → VMs are immutable in Vers; changes force replacement
- `Delete` → calls `DELETE /vm/{id}`

**Exported attributes:**
- `id` — VM ID
- `state` — running/paused/booting
- `ssh_host` — `{id}.vm.vers.sh`
- `ssh_private_key` — from `GET /vm/{id}/ssh_key` (sensitive)
- `ssh_port` — SSH port

### 2.2 `vers_vm_commit` — Snapshot Resource

```hcl
resource "vers_vm_commit" "golden" {
  vm_id = vers_vm.dev.id

  # Trigger re-commit when provisioning changes
  triggers = {
    bootstrap_hash = filesha256("scripts/bootstrap.sh")
    extensions_hash = filesha256("extensions/vers-vm.ts")
  }

  # Keep VM paused after commit (useful for golden images you don't need running)
  keep_paused = false
}
```

**Exported attributes:**
- `commit_id` — The snapshot commit ID
- `vm_id` — Source VM

### 2.3 `vers_vm_branch` — Branch (Clone) Resource

```hcl
resource "vers_vm_branch" "worker_1" {
  source_vm_id = vers_vm.dev.id
}
```

**Exported attributes:**
- `id` — New VM ID
- `source_vm_id` — Parent VM

### 2.4 `vers_vm_restore` — Restore from Commit

```hcl
resource "vers_vm_restore" "from_golden" {
  commit_id = vers_vm_commit.golden.commit_id
}
```

### 2.5 `vers_provisioner` — File & Command Provisioning

Terraform has built-in `provisioner "remote-exec"` and `provisioner "file"`, but Vers VMs use SSH-over-TLS (via `openssl s_client` ProxyCommand), which standard Terraform SSH doesn't support. We need a **custom provisioner** or a **wrapper resource** that handles the Vers SSH transport.

**Option A: Connection block with ProxyCommand** (preferred if Terraform supports it)

```hcl
resource "vers_vm" "golden" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192
  wait_boot    = true

  connection {
    type        = "ssh"
    host        = self.ssh_host
    user        = "root"
    private_key = self.ssh_private_key
    proxy_command = "openssl s_client -connect %h:443 -servername %h -quiet 2>/dev/null"
  }

  provisioner "file" {
    source      = "scripts/bootstrap.sh"
    destination = "/tmp/bootstrap.sh"
  }

  provisioner "remote-exec" {
    inline = [
      "chmod +x /tmp/bootstrap.sh",
      "bash /tmp/bootstrap.sh"
    ]
  }
}
```

> **Note:** Terraform's SSH communicator does not natively support `proxy_command`. This is a known gap. We may need Option B.

**Option B: Custom `vers_provision` resource** (more control)

```hcl
resource "vers_provision" "bootstrap" {
  vm_id = vers_vm.golden.id

  # Files to copy (local → VM)
  file {
    source      = "scripts/bootstrap.sh"
    destination = "/tmp/bootstrap.sh"
  }

  file {
    source      = "extensions/vers-vm.ts"
    destination = "/root/.pi/agent/extensions/vers-vm.ts"
  }

  file {
    source      = "extensions/vers-swarm.ts"
    destination = "/root/.pi/agent/extensions/vers-swarm.ts"
  }

  file {
    content     = templatefile("templates/AGENTS.md", { infra_url = var.infra_url })
    destination = "/root/.pi/agent/context/AGENTS.md"
  }

  # Commands to run (in order)
  commands = [
    "chmod +x /tmp/bootstrap.sh",
    "bash /tmp/bootstrap.sh",
    "mkdir -p /root/.swarm/status",
    "echo '{\"vms\":[]}' > /root/.swarm/registry.json",
  ]

  # Re-provision when these change
  triggers = {
    bootstrap = filesha256("scripts/bootstrap.sh")
    vm_ext    = filesha256("extensions/vers-vm.ts")
    swarm_ext = filesha256("extensions/vers-swarm.ts")
  }
}
```

This resource handles the SSH-over-TLS transport internally (using the same `openssl s_client` ProxyCommand pattern from vers-vm.ts).

---

## 3. Data Sources

### 3.1 `vers_vms` — List VMs

```hcl
data "vers_vms" "all" {}

# Use: data.vers_vms.all.vms[*].vm_id
```

### 3.2 `vers_commit` — Look Up a Commit

```hcl
data "vers_commit" "golden_v2" {
  commit_id = "commit-abc123"
}
```

---

## 4. Provider Configuration

```hcl
terraform {
  required_providers {
    vers = {
      source  = "hdr/vers"
      version = "~> 0.1"
    }
  }
}

provider "vers" {
  api_key  = var.vers_api_key     # or VERS_API_KEY env var
  base_url = "https://api.vers.sh/api/v1"  # optional override
}
```

---

## 5. End-to-End Example: Golden Image Pipeline

```hcl
# variables.tf
variable "vers_api_key" {
  type      = string
  sensitive = true
}

variable "anthropic_api_key" {
  type      = string
  sensitive = true
}

variable "infra_url" {
  type = string
}

variable "auth_token" {
  type      = string
  sensitive = true
}

# golden.tf — Build and commit a golden image
resource "vers_vm" "golden_base" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192
  wait_boot    = true
}

resource "vers_provision" "golden_setup" {
  vm_id = vers_vm.golden_base.id

  file {
    source      = "scripts/bootstrap.sh"
    destination = "/tmp/bootstrap.sh"
  }

  file {
    source      = "extensions/vers-vm.ts"
    destination = "/root/.pi/agent/extensions/vers-vm.ts"
  }

  file {
    source      = "extensions/vers-swarm.ts"
    destination = "/root/.pi/agent/extensions/vers-swarm.ts"
  }

  file {
    source      = "context/AGENTS.md"
    destination = "/root/.pi/agent/context/AGENTS.md"
  }

  file {
    content = <<-EOF
      VERS_INFRA_URL=${var.infra_url}
      VERS_AUTH_TOKEN=${var.auth_token}
    EOF
    destination = "/etc/environment"
  }

  file {
    content = jsonencode({
      vmId       = "PLACEHOLDER"
      agentId    = "PLACEHOLDER"
      rootVmId   = "PLACEHOLDER"
      parentVmId = "PLACEHOLDER"
      depth      = 0
      maxDepth   = 50
      maxVms     = 20
      createdAt  = "PLACEHOLDER"
    })
    destination = "/root/.swarm/identity.json"
  }

  commands = [
    "chmod +x /tmp/bootstrap.sh",
    "bash /tmp/bootstrap.sh",
    "mkdir -p /root/.swarm/status",
    "echo '{\"vms\":[]}' > /root/.swarm/registry.json",
    "touch /root/.swarm/registry.lock",
  ]

  triggers = {
    bootstrap = filesha256("scripts/bootstrap.sh")
  }
}

resource "vers_vm_commit" "golden" {
  vm_id      = vers_vm.golden_base.id
  keep_paused = true

  depends_on = [vers_provision.golden_setup]

  triggers = {
    provision = vers_provision.golden_setup.id
  }
}

# Output the golden commit ID for use by swarm spawners
output "golden_commit_id" {
  value = vers_vm_commit.golden.commit_id
}

# workers.tf — Spawn N workers from the golden image
resource "vers_vm_restore" "worker" {
  count     = var.worker_count
  commit_id = vers_vm_commit.golden.commit_id
}

output "worker_vm_ids" {
  value = vers_vm_restore.worker[*].id
}
```

---

## 6. Implementation Plan

### Phase 1: Provider Skeleton + Core VM Resource
**Language:** Go (standard for Terraform providers, using `terraform-plugin-framework`)

```
vers-tf/
├── PLAN.md                    # This file
├── README.md                  # Usage docs
├── LICENSE
├── go.mod
├── go.sum
├── main.go                    # Provider entry point
├── internal/
│   ├── provider/
│   │   └── provider.go        # Provider config (api_key, base_url)
│   ├── client/
│   │   ├── client.go          # Vers API HTTP client
│   │   └── ssh.go             # SSH-over-TLS execution (openssl ProxyCommand)
│   ├── resources/
│   │   ├── vm.go              # vers_vm resource
│   │   ├── vm_commit.go       # vers_vm_commit resource
│   │   ├── vm_branch.go       # vers_vm_branch resource
│   │   ├── vm_restore.go      # vers_vm_restore resource
│   │   └── provision.go       # vers_provision resource
│   └── datasources/
│       ├── vms.go             # vers_vms data source
│       └── commit.go          # vers_commit data source
├── examples/
│   ├── basic-vm/
│   │   └── main.tf
│   ├── golden-image/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   ├── outputs.tf
│   │   ├── scripts/
│   │   │   └── bootstrap.sh
│   │   └── extensions/
│   │       ├── vers-vm.ts
│   │       └── vers-swarm.ts
│   └── worker-swarm/
│       ├── main.tf
│       ├── variables.tf
│       └── outputs.tf
├── docs/
│   ├── resources/
│   │   ├── vm.md
│   │   ├── vm_commit.md
│   │   ├── vm_branch.md
│   │   ├── vm_restore.md
│   │   └── provision.md
│   └── data-sources/
│       ├── vms.md
│       └── commit.md
└── scripts/
    └── bootstrap.sh           # Reference bootstrap for golden images
```

**Deliverables:**
- [ ] `vers_vm` resource (create, read, delete)
- [ ] Provider config with API key auth
- [ ] Vers API client in Go (list, create, delete, commit, restore, branch, ssh_key)
- [ ] Basic acceptance tests

### Phase 2: Commit + Branch + Restore Resources
- [ ] `vers_vm_commit` resource with triggers
- [ ] `vers_vm_branch` resource
- [ ] `vers_vm_restore` resource
- [ ] Data sources (`vers_vms`, `vers_commit`)

### Phase 3: Provisioning (vers_provision)
- [ ] SSH-over-TLS transport in Go (openssl s_client ProxyCommand)
- [ ] File upload (local → VM via SSH/SCP)
- [ ] Remote command execution
- [ ] `vers_provision` resource with file blocks + commands
- [ ] Template support (content = templatefile(...))
- [ ] Trigger-based re-provisioning

### Phase 4: Polish & Distribution
- [ ] Terraform Registry publishing (registry.terraform.io)
- [ ] Comprehensive examples (golden image, swarm, single VM)
- [ ] Documentation site (auto-generated from schema)
- [ ] CI/CD pipeline for releases (GoReleaser)
- [ ] Import support (adopt existing VMs into state)

---

## 7. Key Design Decisions

### 7.1 SSH-over-TLS Transport

Vers VMs are only reachable via SSH tunneled through TLS (`openssl s_client -connect {host}:443`). Standard Terraform SSH communicator doesn't support this. Two approaches:

**Approach A: Shell out to `ssh` with ProxyCommand** (simpler, proven)
- Same pattern as vers-vm.ts — spawn `ssh` with `-o ProxyCommand=...`
- Requires `ssh` and `openssl` on the machine running Terraform
- Pro: Battle-tested, same code path as the pi extension
- Con: External dependency

**Approach B: Pure Go SSH with TLS dialer** (cleaner)
- Use `golang.org/x/crypto/ssh` with a custom `net.Conn` that does TLS → SSH
- Dial `{vmId}.vm.vers.sh:443` with TLS, then SSH handshake over that connection
- Pro: No external dependencies, fully self-contained
- Con: More complex, need to handle TLS SNI correctly

**Recommendation: Start with Approach A, migrate to B if needed.** The external ssh/openssl dependency is acceptable for v0.1 — every machine running Terraform already has both.

### 7.2 VM Mutability

Vers VMs are fundamentally mutable (you SSH in and run commands), but Terraform resources are declarative. The `vers_vm` resource represents the VM lifecycle only (create/delete). All provisioning is handled by `vers_provision`, which uses triggers to detect when re-provisioning is needed.

This is the same pattern as `aws_instance` + `provisioner` or `null_resource` + triggers.

### 7.3 State Mapping

| Terraform concept | Vers concept |
|---|---|
| `vers_vm` resource | A running VM (`POST /vm/new_root`) |
| `vers_vm_commit` resource | A commit/snapshot (`POST /vm/{id}/commit`) |
| `vers_vm_restore` resource | A VM restored from commit (`POST /vm/from_commit`) |
| `vers_vm_branch` resource | A cloned VM (`POST /vm/{id}/branch`) |
| `terraform destroy` | Delete all managed VMs |
| `terraform import` | Adopt existing VM by ID |

### 7.4 Secrets Handling

- `ssh_private_key` is marked `Sensitive: true` in the schema
- API key comes from provider config or `VERS_API_KEY` env var
- Secrets in provisioning (auth tokens, API keys) use Terraform variables with `sensitive = true`

---

## 8. Comparison with Alternatives

| Approach | Reproducible | Diffable | Composable | Ecosystem |
|---|---|---|---|---|
| Manual scripts | ❌ | ❌ | ❌ | N/A |
| pi golden-vm skill | Partial | ❌ | ❌ | pi only |
| Packer + Vers | ✅ | Partial | ❌ | Packer |
| **Terraform + Vers** | ✅ | ✅ | ✅ | Full HCL |
| Pulumi + Vers | ✅ | ✅ | ✅ | Any language |

Terraform is the best fit because:
1. HCL is purpose-built for infrastructure declarations
2. Massive existing ecosystem and tooling
3. State management is built-in
4. Plan/apply workflow catches mistakes before they happen
5. Most infrastructure teams already know it

---

## 9. Open Questions

1. **VM pause/resume** — Should `vers_vm` support a `state` attribute (running/paused)? Or is that operational and out of Terraform's scope?
   - *Recommendation: Support it.* Pausing golden image VMs after commit saves resources.

2. **Commit garbage collection** — Commits accumulate. Should the provider clean up old commits on destroy?
   - *Recommendation: No.* Commits are cheap and useful for rollback. Let users manage manually.

3. **Vers API versioning** — The API is at `/api/v1`. How do we handle breaking changes?
   - *Recommendation: Pin provider version to API version. Major API changes = major provider version bump.*

4. **Multi-VM orchestration** — Swarms need VMs to discover each other. Should the provider handle registry/service-discovery setup?
   - *Recommendation: Phase 2/3. Start with individual VMs. Swarm coordination (registry, board, feed) is an agent-services concern, not infrastructure.*

5. **Terraform Cloud / remote state** — Should we test with Terraform Cloud for team workflows?
   - *Recommendation: Yes, in Phase 4. Local state first.*

---

## 10. Getting Started (for contributors)

```bash
# Prerequisites
go install github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest

# Clone and build
cd vers-tf
go mod tidy
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

# Run examples
cd examples/basic-vm
export VERS_API_KEY="your-key"
terraform init
terraform plan
terraform apply
```

---

## 11. Success Criteria

### v0.1 (MVP)
- [ ] `terraform apply` creates a Vers VM with specified resources
- [ ] `terraform destroy` cleans it up
- [ ] `vers_provision` can copy files and run commands over SSH-over-TLS
- [ ] `vers_vm_commit` snapshots VMs
- [ ] Golden image example works end-to-end

### v0.5 (Usable)
- [ ] All resources and data sources implemented
- [ ] Trigger-based re-provisioning works
- [ ] Import existing VMs
- [ ] Published to Terraform Registry
- [ ] CI with acceptance tests against real Vers API

### v1.0 (Production)
- [ ] Pure Go SSH transport (no openssl dependency)
- [ ] Terraform Cloud tested
- [ ] Comprehensive docs and examples
- [ ] Stable API contract
