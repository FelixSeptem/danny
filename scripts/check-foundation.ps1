[CmdletBinding()]
param([string]$Root = (Join-Path $PSScriptRoot ".."))

$ErrorActionPreference = "Stop"
Push-Location $Root
try {
    & (Join-Path $Root "scripts/check-provider-boundaries.ps1")
    & (Join-Path $Root "scripts/check-docs-consistency.ps1")
    $modulePath = (& go list -m).Trim()
    if ($modulePath -ne "github.com/FelixSeptem/danny") { throw "Unexpected Go module path: $modulePath" }
    & go list ./...
    & go test ./...
    & go vet ./...
    if (-not (Get-Command golangci-lint -ErrorAction SilentlyContinue)) {
        throw "golangci-lint is required for the quality gate. Install golangci-lint v2 or newer and retry."
    }
    & golangci-lint run ./...
    if ($LASTEXITCODE -ne 0) {
        throw "golangci-lint failed with exit code $LASTEXITCODE"
    }
    Write-Output "Foundation checks passed without contacting a model endpoint."
} finally {
    Pop-Location
}
