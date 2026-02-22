# ============================================================
# Layer 2: Application
#
# Restores from L1 commit, clones your repo, builds, commits.
# ============================================================

resource "vers_vm_restore" "layer2" {
  commit_id = vers_vm_commit.layer1.commit_id
}

resource "vers_provision" "layer2" {
  vm_id = vers_vm_restore.layer2.id

  files = [
    {
      source      = "${path.module}/scripts/app.sh"
      destination = "/tmp/app.sh"
    },
  ]

  commands = [
    "chmod +x /tmp/app.sh",
    "APP_REPO='${var.app_repo}' APP_BRANCH='${var.app_branch}' bash /tmp/app.sh",
  ]

  triggers = {
    script = filesha256("${path.module}/scripts/app.sh")
    repo   = var.app_repo
    branch = var.app_branch
  }
}

resource "vers_vm_commit" "layer2" {
  vm_id       = vers_vm_restore.layer2.id
  keep_paused = true

  depends_on = [vers_provision.layer2]

  triggers = {
    provision = vers_provision.layer2.id
  }
}
