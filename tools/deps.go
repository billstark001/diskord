//go:build tools

package main

// Keep build-time commands and their module checksums in go.mod/go.sum.
import (
	_ "github.com/a-h/templ/cmd/templ"
	_ "golang.org/x/vuln/cmd/govulncheck"
	_ "honnef.co/go/tools/cmd/staticcheck"
)
