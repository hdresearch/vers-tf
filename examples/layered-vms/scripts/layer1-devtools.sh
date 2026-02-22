#!/bin/bash
# Layer 1: Pi agent + swarm infrastructure
set -euo pipefail

echo "=== Layer 1: Dev Tools ==="

# Pi coding agent
npm install -g @mariozechner/pi-coding-agent > /dev/null 2>&1
echo "  pi $(pi --version)"

# Agent directories
mkdir -p /root/.pi/agent/extensions
mkdir -p /root/.pi/agent/context
mkdir -p /root/.swarm/status

# Swarm registry (for multi-agent coordination)
echo '{"vms":[]}' > /root/.swarm/registry.json
touch /root/.swarm/registry.lock

# Identity template (overwritten by swarm spawner)
cat > /root/.swarm/identity.json << 'EOF'
{
  "vmId": "PLACEHOLDER",
  "agentId": "PLACEHOLDER",
  "rootVmId": "PLACEHOLDER",
  "parentVmId": "PLACEHOLDER",
  "depth": 0,
  "maxDepth": 50,
  "maxVms": 20,
  "createdAt": "PLACEHOLDER"
}
EOF

echo "=== Layer 1 complete ==="
