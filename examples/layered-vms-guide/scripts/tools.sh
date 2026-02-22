#!/bin/bash
set -euo pipefail

npm install -g @mariozechner/pi-coding-agent > /dev/null 2>&1

mkdir -p /root/.pi/agent/extensions
mkdir -p /root/.pi/agent/context
mkdir -p /root/.swarm/status

echo '{"vms":[]}' > /root/.swarm/registry.json
touch /root/.swarm/registry.lock

echo "Layer 1 done: pi $(pi --version)"
