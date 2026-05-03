$ErrorActionPreference = "Stop"

$repo = Resolve-Path (Join-Path $PSScriptRoot "..\..\..")
$web = Join-Path $repo "web"

if (-not (Test-Path (Join-Path $web "package.json"))) {
    Write-Output "web/package.json not found; skipping web verification."
    exit 0
}

Set-Location $web

npm run lint
npm run build
