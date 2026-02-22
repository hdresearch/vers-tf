############################################################################
# Layered VMs — 4 layers of branched VMs, each building on the prior one.
#
# Layer 0 (Base OS)     → System packages, Node.js, Git
# Layer 1 (Dev Tools)   → Pi agent, extensions, swarm infrastructure
# Layer 2 (App)         → Clone repo, install deps, build
# Layer 3 (Workers)     → N workers restored from the App layer commit
#
# Each layer is committed as a snapshot. Changes to an early layer
# cascade through all dependent layers on the next `terraform apply`.
############################################################################

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

# ========================================================================
# Layer 0: Base OS
# ========================================================================

resource "vers_vm" "layer0_base" {
  vcpu_count   = 2
  mem_size_mib = 4096
  fs_size_mib  = 8192
  wait_boot    = true
}

resource "vers_provision" "layer0_setup" {
  vm_id = vers_vm.layer0_base.id

  files = [
    {
      source      = "${path.module}/scripts/layer0-base.sh"
      destination = "/tmp/layer0-base.sh"
    },
  ]

  commands = [
    "chmod +x /tmp/layer0-base.sh",
    "bash /tmp/layer0-base.sh",
  ]

  triggers = {
    script = filesha256("${path.module}/scripts/layer0-base.sh")
  }
}

resource "vers_vm_commit" "layer0" {
  vm_id       = vers_vm.layer0_base.id
  keep_paused = true

  depends_on = [vers_provision.layer0_setup]

  triggers = {
    provision = vers_provision.layer0_setup.id
  }
}

# ========================================================================
# Layer 1: Dev Tools (built on Layer 0)
# ========================================================================

resource "vers_vm_restore" "layer1_vm" {
  commit_id = vers_vm_commit.layer0.commit_id
}

resource "vers_provision" "layer1_setup" {
  vm_id = vers_vm_restore.layer1_vm.id

  files = [
    {
      source      = "${path.module}/scripts/layer1-devtools.sh"
      destination = "/tmp/layer1-devtools.sh"
    },
    # Add your extensions here:
    # {
    #   source      = "${path.module}/extensions/vers-vm.ts"
    #   destination = "/root/.pi/agent/extensions/vers-vm.ts"
    # },
    # {
    #   source      = "${path.module}/extensions/vers-swarm.ts"
    #   destination = "/root/.pi/agent/extensions/vers-swarm.ts"
    # },
  ]

  commands = concat(
    [
      "chmod +x /tmp/layer1-devtools.sh",
      "bash /tmp/layer1-devtools.sh",
    ],
    # Bake infra connection into /etc/environment if URL provided
    var.infra_url != "" ? [
      "echo 'VERS_INFRA_URL=${var.infra_url}' >> /etc/environment",
      "echo 'VERS_AUTH_TOKEN=${var.auth_token}' >> /etc/environment",
    ] : [],
  )

  triggers = {
    script    = filesha256("${path.module}/scripts/layer1-devtools.sh")
    infra_url = var.infra_url
  }
}

resource "vers_vm_commit" "layer1" {
  vm_id       = vers_vm_restore.layer1_vm.id
  keep_paused = true

  depends_on = [vers_provision.layer1_setup]

  triggers = {
    provision = vers_provision.layer1_setup.id
  }
}

# ========================================================================
# Layer 2: Application (built on Layer 1)
# ========================================================================

resource "vers_vm_restore" "layer2_vm" {
  commit_id = vers_vm_commit.layer1.commit_id
}

resource "vers_provision" "layer2_setup" {
  vm_id = vers_vm_restore.layer2_vm.id

  files = [
    {
      source      = "${path.module}/scripts/layer2-app.sh"
      destination = "/tmp/layer2-app.sh"
    },
  ]

  commands = [
    "chmod +x /tmp/layer2-app.sh",
    "APP_REPO='${var.app_repo}' APP_BRANCH='${var.app_branch}' bash /tmp/layer2-app.sh",
  ]

  triggers = {
    script = filesha256("${path.module}/scripts/layer2-app.sh")
    repo   = var.app_repo
    branch = var.app_branch
  }
}

resource "vers_vm_commit" "layer2" {
  vm_id       = vers_vm_restore.layer2_vm.id
  keep_paused = true

  depends_on = [vers_provision.layer2_setup]

  triggers = {
    provision = vers_provision.layer2_setup.id
  }
}

# ========================================================================
# Layer 3: Workers (restored from Layer 2 commit)
# ========================================================================

resource "vers_vm_restore" "workers" {
  count     = var.worker_count
  commit_id = vers_vm_commit.layer2.commit_id
}
