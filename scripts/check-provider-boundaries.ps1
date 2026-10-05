[CmdletBinding()]
param(
    [string]$Root = (Join-Path $PSScriptRoot "..")
)

$ErrorActionPreference = "Stop"
$domainDirectories = @(
    "internal/knowledge",
    "internal/tasks",
    "internal/storage",
    "internal/server",
    "web"
)
$providerPatterns = @(
    'github.com/sashabaranov/go-openai',
    'github.com/openai/openai-go',
    'github.com/anthropics/anthropic-sdk-go',
    'google.golang.org/genai',
    'github.com/cohere-ai/cohere-go'
)

$violations = @()
foreach ($directory in $domainDirectories) {
    $path = Join-Path $Root $directory
    if (-not (Test-Path $path)) { continue }
    $files = Get-ChildItem -LiteralPath $path -Recurse -Filter *.go -File
    foreach ($file in $files) {
        $lines = Get-Content -LiteralPath $file.FullName
        foreach ($line in $lines) {
            if ($line -match '^\s*import(?:\s+\w+)?\s+"([^"]+)"' -or $line -match '^\s*"([^"]+)"') {
                $importPath = $Matches[1]
                foreach ($pattern in $providerPatterns) {
                    if ($importPath -eq $pattern) {
                        $violations += "$($file.FullName): prohibited provider import $pattern"
                    }
                }
            }
        }
    }
}

if ($violations.Count -gt 0) {
    $violations | ForEach-Object { Write-Error $_ }
    exit 1
}
Write-Output "Provider boundary check passed."
