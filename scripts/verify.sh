#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
./scripts/lint.sh
ROOT="$PWD"
mkdir -p .build/{tmp,gocache,modcache,gopath}
export GOTMPDIR="$ROOT/.build/tmp" TMPDIR="$ROOT/.build/tmp" TMP="$ROOT/.build/tmp" TEMP="$ROOT/.build/tmp"
export GOCACHE="$ROOT/.build/gocache" GOMODCACHE="$ROOT/.build/modcache" GOPATH="$ROOT/.build/gopath"
export GOTOOLCHAIN=local GOTELEMETRY=off
go test -race ./...
