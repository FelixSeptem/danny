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
$readme = Get-Content -Raw -LiteralPath (Join-Path $Root "README.md")
foreach ($match in [regex]::Matches($readme, 'scripts[\\/][A-Za-z0-9._-]+\.ps1')) {
    $relative = $match.Value -replace '/', '\'
    if (-not (Test-Path -LiteralPath (Join-Path $Root $relative))) {
        throw "README.md references a script that does not exist: $relative"
    }
}
Write-Output "Documentation consistency check passed."
