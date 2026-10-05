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
    Write-Output "Foundation checks passed without contacting a model endpoint."
} finally {
    Pop-Location
}
