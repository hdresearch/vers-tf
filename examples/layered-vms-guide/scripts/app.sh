#!/bin/bash
set -euo pipefail

APP_REPO="${APP_REPO:?APP_REPO required}"
APP_BRANCH="${APP_BRANCH:-main}"

cd /root/workspace
git clone --branch "$APP_BRANCH" "$APP_REPO" app
cd app

if [ -f "package-lock.json" ]; then
  npm ci > /dev/null 2>&1
fi

if [ -f "package.json" ] && grep -q '"build"' package.json; then
  npm run build > /dev/null 2>&1
fi

echo "Layer 2 done: app cloned and built"
