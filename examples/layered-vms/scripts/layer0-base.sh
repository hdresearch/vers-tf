#!/bin/bash
# Layer 0: Base OS packages
set -euo pipefail

echo "=== Layer 0: Base OS ==="

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq \
  git curl wget build-essential \
  ripgrep fd-find jq tree \
  python3 python3-pip \
  openssh-client \
  ca-certificates gnupg \
  > /dev/null 2>&1

ln -sf "$(which fdfind)" /usr/local/bin/fd 2>/dev/null || true

# Node.js 22 LTS
if ! command -v node &>/dev/null; then
  curl -fsSL https://deb.nodesource.com/setup_22.x | bash - > /dev/null 2>&1
  apt-get install -y -qq nodejs > /dev/null 2>&1
fi

# Git config
git config --global user.name "pi-agent"
git config --global user.email "agent@vers.sh"
git config --global init.defaultBranch main
git config --global core.editor "true"
echo 'export GIT_EDITOR=true' >> /root/.bashrc
git config --global merge.commit no-edit

# Workspace
mkdir -p /root/workspace

echo "=== Layer 0 complete: node $(node --version) ==="
