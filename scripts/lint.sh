#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"
mkdir -p .build/{tmp,gocache,modcache,gopath}
export GOTMPDIR="$ROOT/.build/tmp" TMPDIR="$ROOT/.build/tmp" TMP="$ROOT/.build/tmp" TEMP="$ROOT/.build/tmp"
export GOCACHE="$ROOT/.build/gocache" GOMODCACHE="$ROOT/.build/modcache" GOPATH="$ROOT/.build/gopath"
export GOTOOLCHAIN=local GOTELEMETRY=off

unformatted="$(gofmt -l cmd internal tools)"
if [ -n "$unformatted" ]; then
  printf 'Go files need gofmt:\n%s\n' "$unformatted" >&2
  exit 1
fi
go run github.com/a-h/templ/cmd/templ fmt -fail internal/ui
go run github.com/a-h/templ/cmd/templ generate -check -path internal/ui
go mod verify
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck ./...
