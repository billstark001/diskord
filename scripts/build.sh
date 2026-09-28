#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"
mkdir -p .build/{tmp,gocache,modcache,gopath} dist
export GOTMPDIR="$ROOT/.build/tmp" TMPDIR="$ROOT/.build/tmp" TMP="$ROOT/.build/tmp" TEMP="$ROOT/.build/tmp"
export GOCACHE="$ROOT/.build/gocache" GOMODCACHE="$ROOT/.build/modcache" GOPATH="$ROOT/.build/gopath"
export GOTOOLCHAIN=local GOTELEMETRY=off
TARGET_OS="${TARGET_GOOS:-$(go env GOOS)}"
TARGET_ARCH="${TARGET_GOARCH:-$(go env GOARCH)}"
# Generators run on the host; only the final build is cross-compiled.
unset GOOS GOARCH
go run ./tools/prepare.go
go run github.com/a-h/templ/cmd/templ generate -path internal/ui
go mod tidy
go test ./...
SUFFIX=""; [ "$TARGET_OS" != windows ] || SUFFIX=.exe
CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -trimpath -ldflags='-s -w -buildid=' -o "dist/diskord-${TARGET_OS}-${TARGET_ARCH}${SUFFIX}" ./cmd/diskord
printf '\nBuilt: dist/diskord-%s-%s%s\n' "$TARGET_OS" "$TARGET_ARCH" "$SUFFIX"
printf 'The binary embeds the UI and htmx. Node/npm is not required.\n'
