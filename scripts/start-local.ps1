[CmdletBinding()]
param(
    [int]$Port = 8080,
    [string]$Root = (Join-Path $PSScriptRoot ".."),
    [switch]$SkipFrontend,
    [switch]$CheckOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = (Resolve-Path -LiteralPath $Root).Path
$webRoot = Join-Path $repoRoot "web"
$distRoot = Join-Path $webRoot "dist"

function Require-Tool {
    param([string]$Name, [string]$Hint)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "$Name is required to start Danny locally. $Hint"
    }
}

Require-Tool "go" "Install Go 1.26 or newer from https://go.dev/dl/."
Require-Tool "node" "Install Node.js 20 or newer from https://nodejs.org/."
Require-Tool "npm" "npm ships with Node.js; see https://nodejs.org/."

Write-Output "Toolchain:"
foreach ($tool in @("go", "node", "npm")) {
    $version = (& $tool --version 2>$null | Select-Object -First 1)
    Write-Output ("  {0,-5} {1}" -f $tool, $version)
}

$assetsReady = Test-Path -LiteralPath (Join-Path $distRoot "index.html")
if ($assetsReady) {
    Write-Output "Workbench assets: present at web/dist"
} else {
    Write-Output "Workbench assets: missing at web/dist"
}

if ($CheckOnly) {
    Write-Output "Check-only mode: stopping before the front-end build and the service start."
    return
}

if (-not $SkipFrontend) {
    Push-Location $webRoot
    try {
        if (-not (Test-Path -LiteralPath (Join-Path $webRoot "node_modules"))) {
            Write-Output "Installing the pinned front-end dependencies..."
            & npm ci --no-audit --no-fund
            if ($LASTEXITCODE -ne 0) { throw "npm ci failed with exit code $LASTEXITCODE" }
        }
        Write-Output "Building the workbench..."
        & npm run build
        if ($LASTEXITCODE -ne 0) { throw "npm run build failed with exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

$env:DANNY_LISTEN_ADDRESS = "127.0.0.1:$Port"
$env:DANNY_DATA_DIR = Join-Path $repoRoot "data"
$env:DANNY_DOCUMENTS_DIR = Join-Path $repoRoot "data/documents"
$env:DANNY_ARTIFACTS_DIR = Join-Path $repoRoot "data/artifacts"
$env:DANNY_DATABASE_URL = Join-Path $repoRoot "data/danny.db"
$env:DANNY_WORKBENCH_DIR = $distRoot

Write-Output ""
Write-Output "Danny workbench:"
Write-Output "  URL:    http://127.0.0.1:$Port/"
Write-Output "  Health: http://127.0.0.1:$Port/api/health"
Write-Output "  Ready:  http://127.0.0.1:$Port/api/health/ready"
Write-Output "  Stop:   press Ctrl+C"
Write-Output ""

Push-Location $repoRoot
try {
    & go run ./cmd/personal-agent
    if ($LASTEXITCODE -ne 0) { throw "the service exited with code $LASTEXITCODE" }
} finally {
    Pop-Location
}
