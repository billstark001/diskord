//go:build tools

package main

// Keep build-time commands and their module checksums in go.mod/go.sum.
import (
	_ "golang.org/x/vuln/cmd/govulncheck"
	_ "honnef.co/go/tools/cmd/staticcheck"
)
