[CmdletBinding()]
param([string]$Root = (Join-Path $PSScriptRoot ".."))

$ErrorActionPreference = "Stop"
$required = @(
    "README.md",
    "docs/upstream/baymax-findings.md",
    "docs/upstream/stele-findings.md",
    "testdata/fixtures/manifest.json"
)
foreach ($relativePath in $required) {
    if (-not (Test-Path (Join-Path $Root $relativePath))) {
        throw "Required documentation or fixture file is missing: $relativePath"
    }
}
$manifestPath = Join-Path $Root "testdata/fixtures/manifest.json"
$manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json
if ($manifest.dependency_versions.baymax -notmatch '^v0\.0\.0-') { throw "Baymax manifest version is not a pseudo-version" }
if ($manifest.dependency_versions.stele -notmatch '^v0\.0\.0-') { throw "Stele manifest version is not a pseudo-version" }
Write-Output "Documentation consistency check passed."
