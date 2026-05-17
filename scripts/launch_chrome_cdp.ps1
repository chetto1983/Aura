# launch_chrome_cdp.ps1 — start a dedicated Chrome instance with remote
# debugging enabled so cmd/probe_telegram_ui can attach to it.
#
# This uses a SEPARATE profile dir so it doesn't conflict with your normal
# Chrome session. On first run, log into https://web.telegram.org/a/ in
# this Chrome and open the Aura bot chat. The profile is persisted, so
# subsequent runs reuse the login.
#
# Usage:
#   powershell.exe -ExecutionPolicy Bypass -File .\scripts\launch_chrome_cdp.ps1
#
# After this, in another shell:
#   go run ./cmd/probe_telegram_ui -prompt "trova in memoria phantom guard"

$ErrorActionPreference = "Stop"

$chromeCandidates = @(
    "${env:ProgramFiles}\Google\Chrome\Application\chrome.exe",
    "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
    "${env:LOCALAPPDATA}\Google\Chrome\Application\chrome.exe"
)
$chromeExe = $null
foreach ($p in $chromeCandidates) {
    if (Test-Path $p) { $chromeExe = $p; break }
}
if (-not $chromeExe) {
    Write-Error "Chrome not found in standard locations. Edit \$chromeCandidates above."
    exit 1
}

$profileDir = Join-Path $env:USERPROFILE ".chrome-cdp-profile"
if (-not (Test-Path $profileDir)) {
    New-Item -ItemType Directory -Path $profileDir | Out-Null
    Write-Host "Created dedicated profile dir: $profileDir"
}

# Probe if CDP is already up.
try {
    $r = Invoke-WebRequest -Uri "http://127.0.0.1:9222/json/version" -TimeoutSec 2 -UseBasicParsing
    if ($r.StatusCode -eq 200) {
        Write-Host "Chrome CDP already listening on http://127.0.0.1:9222"
        Write-Host "Targets:"
        $tabs = Invoke-WebRequest -Uri "http://127.0.0.1:9222/json/list" -TimeoutSec 2 -UseBasicParsing | ConvertFrom-Json
        $tabs | Where-Object { $_.type -eq "page" } | ForEach-Object { Write-Host "  - $($_.url)" }
        exit 0
    }
} catch { } # not running — start it

Write-Host "Launching Chrome with CDP on port 9222"
Write-Host "  exe:     $chromeExe"
Write-Host "  profile: $profileDir"
Start-Process -FilePath $chromeExe -ArgumentList @(
    "--remote-debugging-port=9222",
    "--user-data-dir=`"$profileDir`"",
    "--no-first-run",
    "--no-default-browser-check",
    "https://web.telegram.org/a/"
)

Write-Host ""
Write-Host "Chrome launched. Next steps:"
Write-Host "  1. Log into Telegram inside this Chrome (it's a fresh profile)"
Write-Host "  2. Open the chat with your Aura bot"
Write-Host "  3. Run:  go run ./cmd/probe_telegram_ui -prompt `"trova in memoria phantom guard`""
