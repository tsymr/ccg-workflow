#!/usr/bin/env bash
# Rebuild CCG locally: compile the Go wrapper binary into bin/ and build the TS dist.
# After it finishes, run `ccg init` to push the new binary + templates into ~/.claude.
#
# Usage:  ./rebuild.sh
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_DIR"

# --- detect platform for the binary name (matches src getBinaryName) ---
case "$(uname -s)" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *) echo "Unsupported OS: $(uname -s)"; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64)  ARCH=amd64 ;;
  arm64 | aarch64) ARCH=arm64 ;;
  *) echo "Unsupported arch: $(uname -m)"; exit 1 ;;
esac
BIN_OUT="$REPO_DIR/bin/codeagent-wrapper-${OS}-${ARCH}"

# --- locate Go: prefer PATH, fall back to ~/sdk/go ---
if ! command -v go >/dev/null 2>&1; then
  export GOROOT="$HOME/sdk/go"
  export PATH="$GOROOT/bin:$PATH"
fi
# go.mod pins `go 1.21`; keep the locally-installed toolchain instead of fetching one.
export GOTOOLCHAIN=local

echo "==> [1/3] Building Go wrapper -> bin/codeagent-wrapper-${OS}-${ARCH}"
go build -C "$REPO_DIR/codeagent-wrapper" -o "$BIN_OUT" .
"$BIN_OUT" --version

echo "==> [2/3] Ensuring TS deps"
[ -d node_modules ] || pnpm install

echo "==> [3/3] Building TypeScript dist (pnpm build)"
pnpm build

echo
echo "✅ Rebuilt. Now run:  ccg init   # installs the new binary + templates into ~/.claude"
