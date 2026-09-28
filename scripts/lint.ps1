$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$root = (Get-Location).Path
foreach ($dir in @(".build/tmp", ".build/gocache", ".build/modcache", ".build/gopath")) {
    New-Item -ItemType Directory -Force $dir | Out-Null
}
$env:GOTMPDIR = Join-Path $root ".build/tmp"
$env:TMPDIR = $env:GOTMPDIR; $env:TMP = $env:GOTMPDIR; $env:TEMP = $env:GOTMPDIR
$env:GOCACHE = Join-Path $root ".build/gocache"
$env:GOMODCACHE = Join-Path $root ".build/modcache"
$env:GOPATH = Join-Path $root ".build/gopath"
$env:GOTOOLCHAIN = "local"; $env:GOTELEMETRY = "off"
function Invoke-Go {
    & go @args
    if ($LASTEXITCODE -ne 0) { throw "go command failed with exit code $LASTEXITCODE" }
}
$unformatted = & gofmt -l cmd internal tools
if ($LASTEXITCODE -ne 0) { throw "gofmt failed" }
if ($unformatted) { throw "Go files need gofmt: $($unformatted -join ', ')" }
& node scripts/build-frontend.mjs
if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }
& pnpm -C frontend run check
if ($LASTEXITCODE -ne 0) { throw "frontend check failed" }
Invoke-Go mod verify
Invoke-Go vet ./...
Invoke-Go run honnef.co/go/tools/cmd/staticcheck ./...
