#!/bin/bash
# Layer 2: Application — clone repo, install deps, build
set -euo pipefail

echo "=== Layer 2: Application ==="

APP_REPO="${APP_REPO:-https://github.com/example/app.git}"
APP_BRANCH="${APP_BRANCH:-main}"

cd /root/workspace

# Clone the application repo
if [ -d "app" ]; then
  echo "Updating existing repo..."
  cd app
  git fetch origin
  git checkout "$APP_BRANCH"
  git pull origin "$APP_BRANCH"
else
  echo "Cloning $APP_REPO ($APP_BRANCH)..."
  git clone --branch "$APP_BRANCH" "$APP_REPO" app
  cd app
fi

# Install dependencies (detect package manager)
if [ -f "package-lock.json" ]; then
  echo "Installing npm dependencies..."
  npm ci > /dev/null 2>&1
elif [ -f "yarn.lock" ]; then
  echo "Installing yarn dependencies..."
  npm install -g yarn > /dev/null 2>&1
  yarn install --frozen-lockfile > /dev/null 2>&1
elif [ -f "pnpm-lock.yaml" ]; then
  echo "Installing pnpm dependencies..."
  npm install -g pnpm > /dev/null 2>&1
  pnpm install --frozen-lockfile > /dev/null 2>&1
elif [ -f "requirements.txt" ]; then
  echo "Installing pip dependencies..."
  pip3 install -r requirements.txt > /dev/null 2>&1
fi

# Build if there's a build script
if [ -f "package.json" ] && grep -q '"build"' package.json 2>/dev/null; then
  echo "Building..."
  npm run build > /dev/null 2>&1
fi

echo "=== Layer 2 complete ==="
