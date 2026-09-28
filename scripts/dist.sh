#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"
# Remove the ZIP produced by the previous local packaging rule.
if [ -f dist/diskord-v0.1-complete.zip ]; then
  rm dist/diskord-v0.1-complete.zip
fi
./scripts/build.sh

# Cross-compiled executables are the local distribution output. GitHub
# supplies source archives automatically when a tagged Release is published.
mkdir -p .build/{tmp,gocache,modcache,gopath} dist
export GOTMPDIR="$ROOT/.build/tmp" TMPDIR="$ROOT/.build/tmp" TMP="$ROOT/.build/tmp" TEMP="$ROOT/.build/tmp"
export GOCACHE="$ROOT/.build/gocache" GOMODCACHE="$ROOT/.build/modcache" GOPATH="$ROOT/.build/gopath"
export GOTOOLCHAIN=local GOTELEMETRY=off
for target in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64; do
  target_os="${target%/*}"
  target_arch="${target#*/}"
  suffix=""
  [ "$target_os" != windows ] || suffix=.exe
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags='-s -w -buildid=' -o "dist/diskord-${target_os}-${target_arch}${suffix}" ./cmd/diskord
done
printf 'Executables are in dist/. No local source archive was created.\n'
