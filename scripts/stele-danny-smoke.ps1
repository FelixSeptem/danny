[CmdletBinding()]
param(
    [string]$ProjectName = "danny-stele-smoke-$([guid]::NewGuid().ToString('N').Substring(0, 10))",
    [string]$SteleRepository = "D:\code\stele",
    [string]$Image = "stele-f38016-verify:local",
    [string]$ReportPath = "testdata/runtimeprobe/stele-f38016-danny-smoke.evidence.json"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $SteleRepository "docker-compose.yml"
$credentialDirectory = Join-Path ([IO.Path]::GetTempPath()) "stele-danny-credentials-$ProjectName"
$overridePath = Join-Path ([IO.Path]::GetTempPath()) "stele-danny-$ProjectName.yml"
$started = $false
$overrideCreated = $false
$previousVariables = @{}
$variables = @("COMPOSE_PROJECT_NAME", "STELE_POSTGRES_IMAGE", "STELE_POSTGRES_PASSWORD", "STELE_AUTH_BOOTSTRAP_ADMIN_KEY", "STELE_AUTH_DEFAULT_TENANT", "STELE_AUTH_DEFAULT_PROJECT", "STELE_AUTH_DEFAULT_NAMESPACE", "STELE_POSTGRES_HOST_PORT", "STELE_HTTP_HOST_PORT", "STELE_URL", "STELE_CREDENTIAL_FILE", "STELE_TENANT", "STELE_PROJECT", "STELE_NAMESPACE")
foreach ($variable in $variables) { $previousVariables[$variable] = [Environment]::GetEnvironmentVariable($variable, "Process") }

try {
    if ($ProjectName -notmatch '^[a-z0-9][a-z0-9_-]{2,50}$') { throw "invalid smoke project name" }
    if (-not (Test-Path -LiteralPath $composeFile)) { throw "Stele compose file is missing" }
    $reportPath = [IO.Path]::GetFullPath((Join-Path $repoRoot $ReportPath))
    $reportDirectory = Split-Path -Parent $reportPath
    New-Item -ItemType Directory -Force -Path $reportDirectory | Out-Null

    $env:COMPOSE_PROJECT_NAME = $ProjectName
    $composeArgs = @("compose", "-f", $composeFile)
    $existing = (& docker @composeArgs ps -aq | Out-String).Trim()
    $existingVolumes = (& docker volume ls --filter "label=com.docker.compose.project=$ProjectName" -q | Out-String).Trim()
    if ($existing -or $existingVolumes) { throw "smoke project already owns Docker resources" }

    $imageDigest = (& docker image inspect --format '{{.Id}}' $Image | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $imageDigest -notmatch '^sha256:[a-f0-9]{64}$') { throw "verified Stele image is unavailable" }
    $env:STELE_POSTGRES_IMAGE = "docker.1ms.run/pgvector/pgvector:pg18"
    $env:STELE_POSTGRES_PASSWORD = "smoke-$([guid]::NewGuid().ToString('N'))"
    $env:STELE_AUTH_BOOTSTRAP_ADMIN_KEY = "smoke-bootstrap-$([guid]::NewGuid().ToString('N'))"
    $env:STELE_AUTH_DEFAULT_TENANT = "tenant-$ProjectName"
    $env:STELE_AUTH_DEFAULT_PROJECT = "project-$ProjectName"
    $env:STELE_AUTH_DEFAULT_NAMESPACE = "namespace-$ProjectName"
    $env:STELE_POSTGRES_HOST_PORT = (Get-Random -Minimum 25432 -Maximum 35432).ToString()
    $env:STELE_HTTP_HOST_PORT = (Get-Random -Minimum 28080 -Maximum 38080).ToString()
    $baseUrl = "http://127.0.0.1:$($env:STELE_HTTP_HOST_PORT)"

    @(
        "services:",
        "  api:",
        "    image: $imageDigest",
        "    environment:",
        "      STELE_PROVIDER_ENABLED: `"true`"",
        "      STELE_PROVIDER_SCHEMA_VERSIONS: `"provider-v1`"",
        "  worker:",
        "    image: $imageDigest",
        "    environment:",
        "      STELE_PROVIDER_ENABLED: `"true`"",
        "  scheduler:",
        "    image: $imageDigest",
        "    environment:",
        "      STELE_PROVIDER_ENABLED: `"true`""
    ) | Set-Content -LiteralPath $overridePath -Encoding UTF8
    $overrideCreated = $true
    $composeArgs += @("-f", $overridePath)

    $started = $true
    & docker @composeArgs up -d --no-build
    if ($LASTEXITCODE -ne 0) { throw "isolated Stele stack failed to start" }

    $deadline = [DateTime]::UtcNow.AddSeconds(60)
    do {
        try { if ((Invoke-WebRequest -Uri "$baseUrl/readyz" -ErrorAction Stop).StatusCode -eq 200) { break } } catch { }
        Start-Sleep -Milliseconds 300
    } while ([DateTime]::UtcNow -lt $deadline)
    if ([DateTime]::UtcNow -ge $deadline) { throw "isolated Stele API readiness timed out" }

    $vector = (& docker @composeArgs exec -T postgres psql -U stele -d stele -At -c "SELECT extversion FROM pg_extension WHERE extname='vector'" | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $vector) { throw "isolated database has no pgvector extension" }

    & pwsh -NoProfile -File (Join-Path $SteleRepository "scripts/stele-bootstrap-smoke.ps1") -BaseUrl $baseUrl -BootstrapKey $env:STELE_AUTH_BOOTSTRAP_ADMIN_KEY -Tenant $env:STELE_AUTH_DEFAULT_TENANT -Project $env:STELE_AUTH_DEFAULT_PROJECT -Namespace $env:STELE_AUTH_DEFAULT_NAMESPACE -CredentialOutputDirectory $credentialDirectory -DisclosureProfile memory-reference-v1 -SkipLifecycle
    if ($LASTEXITCODE -ne 0) { throw "official runtime principal bootstrap failed" }

    Push-Location $repoRoot
    try {
        $env:STELE_URL = $baseUrl
        $env:STELE_CREDENTIAL_FILE = Join-Path $credentialDirectory "runtime.credential"
        $env:STELE_TENANT = $env:STELE_AUTH_DEFAULT_TENANT
        $env:STELE_PROJECT = $env:STELE_AUTH_DEFAULT_PROJECT
        $env:STELE_NAMESPACE = $env:STELE_AUTH_DEFAULT_NAMESPACE
        & go run ./cmd/runtime-probe stele-smoke | Out-File -LiteralPath $reportPath -Encoding utf8
        if ($LASTEXITCODE -ne 0) { throw "Danny Stele smoke failed" }
        $bundle = Get-Content -Raw -LiteralPath $reportPath | ConvertFrom-Json
        if ($bundle.schema_version -ne "runtime-probe.v1" -or $bundle.stele_version -ne "v0.0.0-20261025080141-f38016ec0cba") { throw "Danny evidence pin or schema mismatch" }
        foreach ($capability in @("context", "memory", "references", "compilation", "restart", "idempotent")) {
            $events = @($bundle.events | Where-Object { $_.capability -eq $capability })
            if (-not $events -or @($events | Where-Object { $_.kind -eq "terminal" -and $_.status -eq "passed" }).Count -ne 1) {
                throw "Danny Stele capability failed: $capability"
            }
        }
    } finally {
        Pop-Location
    }
    Write-Output "PASS: Danny stele-smoke against Stele $($bundle.stele_version), pgvector $vector. Evidence: $reportPath"
} catch {
    throw "Danny Stele live smoke failed; no raw response or credential was retained."
} finally {
    if ($started) { & docker @composeArgs down --volumes --remove-orphans *> $null }
    if ($overrideCreated -and (Test-Path -LiteralPath $overridePath)) { Remove-Item -LiteralPath $overridePath -Force }
    if (Test-Path -LiteralPath $credentialDirectory) { Remove-Item -LiteralPath $credentialDirectory -Recurse -Force }
    foreach ($variable in $variables) { [Environment]::SetEnvironmentVariable($variable, $previousVariables[$variable], "Process") }
}
