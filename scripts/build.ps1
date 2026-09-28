param([string]$TargetOS = "windows", [string]$TargetArch = "amd64")
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$root = (Get-Location).Path
foreach ($dir in @(".build/tmp", ".build/gocache", ".build/modcache", ".build/gopath", "dist")) {
    New-Item -ItemType Directory -Force $dir | Out-Null
}
$env:GOTMPDIR = Join-Path $root ".build/tmp"
$env:TMPDIR = $env:GOTMPDIR; $env:TMP = $env:GOTMPDIR; $env:TEMP = $env:GOTMPDIR
$env:GOCACHE = Join-Path $root ".build/gocache"
$env:GOMODCACHE = Join-Path $root ".build/modcache"
$env:GOPATH = Join-Path $root ".build/gopath"
$env:GOTOOLCHAIN = "local"; $env:GOTELEMETRY = "off"
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
function Invoke-Go {
    & go @args
    if ($LASTEXITCODE -ne 0) { throw "go command failed with exit code $LASTEXITCODE" }
}
Invoke-Go run ./tools/prepare.go
Invoke-Go run github.com/a-h/templ/cmd/templ generate -path internal/ui
Invoke-Go mod tidy
Invoke-Go test ./...
$env:CGO_ENABLED = "0"; $env:GOOS = $TargetOS; $env:GOARCH = $TargetArch
$suffix = if ($TargetOS -eq "windows") { ".exe" } else { "" }
$output = "dist/diskord-$TargetOS-$TargetArch$suffix"
Invoke-Go build -trimpath '-ldflags=-s -w -buildid=' -o $output ./cmd/diskord
Write-Output "Built: $output"
Write-Output "The binary embeds the UI and htmx. Node/npm is not required."
