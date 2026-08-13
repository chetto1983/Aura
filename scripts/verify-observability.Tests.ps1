[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$sourceRoot = Split-Path -Parent $PSScriptRoot
$verifierPath = Join-Path $PSScriptRoot 'verify-observability.ps1'
$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("aura-observability-tests-" + [guid]::NewGuid().ToString('N'))

function Copy-ContractFixture {
    param([Parameter(Mandatory)] [string]$Name)

    $root = Join-Path $tempRoot $Name
    New-Item -ItemType Directory -Path $root -Force | Out-Null
    foreach ($relative in @('internal/obs/catalog.go', 'observability', 'compose.yaml', '.github/workflows/ci.yml')) {
        $source = Join-Path $sourceRoot $relative
        $target = Join-Path $root $relative
        $parent = Split-Path -Parent $target
        New-Item -ItemType Directory -Path $parent -Force | Out-Null
        Copy-Item -LiteralPath $source -Destination $target -Recurse -Force
    }
    return $root
}

function Set-ContractText {
    param(
        [Parameter(Mandatory)] [string]$Root,
        [Parameter(Mandatory)] [string]$RelativePath,
        [Parameter(Mandatory)] [string]$Old,
        [Parameter(Mandatory)] [string]$New
    )

    $path = Join-Path $Root $RelativePath
    $text = Get-Content -LiteralPath $path -Raw
    if (-not $text.Contains($Old)) {
        throw "fixture mutation source not found in ${RelativePath}: $Old"
    }
    [System.IO.File]::WriteAllText($path, $text.Replace($Old, $New), [System.Text.UTF8Encoding]::new($false))
}

function Invoke-ContractVerification {
    param([Parameter(Mandatory)] [string]$Root)

    try {
        $output = & $verifierPath -RepositoryRoot $Root -StaticOnly -SkipContainerTools 2>&1 | Out-String
        return [pscustomobject]@{ Passed = $true; Output = $output }
    } catch {
        # Keep the exception message as raw text. Windows PowerShell's formatter
        # wraps ErrorRecord output to the host width, which can insert newlines in
        # the middle of the exact contract substring and make a correct negative
        # failure look imprecise.
        return [pscustomobject]@{ Passed = $false; Output = $_.Exception.Message }
    }
}

function Assert-NegativeCase {
    param(
        [Parameter(Mandatory)] [string]$Name,
        [Parameter(Mandatory)] [scriptblock]$Mutate,
        [Parameter(Mandatory)] [string]$Expected
    )

    $root = Copy-ContractFixture -Name $Name
    & $Mutate $root
    $result = Invoke-ContractVerification -Root $root
    if ($result.Passed) {
        throw "negative fixture '$Name' unexpectedly passed"
    }
    if (-not $result.Output.Contains($Expected)) {
        throw "negative fixture '$Name' failed imprecisely; expected '$Expected', got: $($result.Output)"
    }
    Write-Host "negative fixture passed: $Name -> $Expected"
}

try {
    New-Item -ItemType Directory -Path $tempRoot -Force | Out-Null

    $baselineRoot = Copy-ContractFixture -Name 'baseline'
    $baseline = Invoke-ContractVerification -Root $baselineRoot
    if (-not $baseline.Passed) {
        throw "baseline observability contract failed: $($baseline.Output)"
    }

    Assert-NegativeCase -Name 'invalid-yaml' -Expected 'invalid YAML' -Mutate {
        param($root)
        Set-ContractText $root 'observability/grafana/provisioning/datasources/aura.yml' 'apiVersion: 1' 'apiVersion: ['
    }
    Assert-NegativeCase -Name 'invalid-json' -Expected 'invalid dashboard JSON' -Mutate {
        param($root)
        Set-ContractText $root 'observability/grafana/dashboards/aura-overview.json' '"uid": "aura-overview"' '"uid": "aura-overview", BROKEN'
    }
    Assert-NegativeCase -Name 'duplicate-uids' -Expected 'duplicate dashboard UID aura-overview' -Mutate {
        param($root)
        Set-ContractText $root 'observability/grafana/dashboards/aura-agents.json' '"uid": "aura-agents"' '"uid": "aura-overview"'
    }
    Assert-NegativeCase -Name 'unknown-metric' -Expected 'unknown metric aura_unknown_total' -Mutate {
        param($root)
        Set-ContractText $root 'observability/grafana/dashboards/aura-agents.json' 'aura:llm_latency_p95_seconds' 'aura_unknown_total'
    }
    Assert-NegativeCase -Name 'unknown-label' -Expected 'forbidden label user_id' -Mutate {
        param($root)
        Set-ContractText $root 'observability/grafana/dashboards/aura-agents.json' 'aura_agent_llm_call_total[5m]' 'aura_agent_llm_call_total{user_id=\"sensitive\"}[5m]'
    }
    Assert-NegativeCase -Name 'alert-panel-link' -Expected 'references missing panel aura-overview/999' -Mutate {
        param($root)
        Set-ContractText $root 'observability/prometheus/rules/aura-alerts.yml' 'panel_id: "1"' 'panel_id: "999"'
    }
    Assert-NegativeCase -Name 'runbook-link' -Expected 'references missing runbook observability/runbooks/missing.md' -Mutate {
        param($root)
        Set-ContractText $root 'observability/prometheus/rules/aura-alerts.yml' 'runbook_url: observability/runbooks/aura-readiness.md' 'runbook_url: observability/runbooks/missing.md'
    }
    # Proves the inverted namespace assertion is not vacuous. Re-introducing the sharing
    # that was removed on 2026-08-13 must fail the contract: it is the arrangement that
    # left up{job="aura"} at 0 for five and a half hours with nobody informed, because
    # Compose never re-attaches a sharer after the namespace owner is recreated
    # (docker/compose#6626).
    Assert-NegativeCase -Name 'shared-network-namespace' -Expected 'must NOT share a network namespace' -Mutate {
        param($root)
        Set-ContractText $root 'compose.yaml' '    restart: unless-stopped
    depends_on:
      aura:
        condition: service_healthy
    command:
      - --config.file=/etc/prometheus/prometheus.yml' '    restart: unless-stopped
    network_mode: "service:aura"
    depends_on:
      aura:
        condition: service_healthy
    command:
      - --config.file=/etc/prometheus/prometheus.yml'
    }
    Assert-NegativeCase -Name 'unpinned-image' -Expected 'Prometheus image must be digest-pinned' -Mutate {
        param($root)
        $path = Join-Path $root 'compose.yaml'
        $text = Get-Content -LiteralPath $path -Raw
        $mutated = [regex]::Replace(
            $text,
            '(?m)^(    image: prom/prometheus:v3\.13\.1)@sha256:[0-9a-f]{64}\r?$',
            '$1',
            1
        )
        if ($mutated -eq $text) {
            throw 'fixture mutation source not found in compose.yaml: Prometheus digest'
        }
        [System.IO.File]::WriteAllText($path, $mutated, [System.Text.UTF8Encoding]::new($false))
    }
    Assert-NegativeCase -Name 'published-ingestion' -Expected 'Tempo ingestion port 4317 must not be host-published' -Mutate {
        param($root)
        $path = Join-Path $root 'compose.yaml'
        $text = Get-Content -LiteralPath $path -Raw
        $replacement = '  tempo:' + [Environment]::NewLine + '    ports: ["4317:4317"]'
        $text = [regex]::Replace($text, '(?m)^  tempo:\r?$', $replacement, 1)
        [System.IO.File]::WriteAllText($path, $text, [System.Text.UTF8Encoding]::new($false))
    }
    Assert-NegativeCase -Name 'broken-provisioning-path' -Expected 'dashboard provider path does not match the Grafana mount' -Mutate {
        param($root)
        Set-ContractText $root 'observability/grafana/provisioning/dashboards/aura.yml' '/var/lib/grafana/dashboards' '/var/lib/grafana/missing'
    }

    Write-Host 'observability verifier fixture suite passed'
} finally {
    if (Test-Path -LiteralPath $tempRoot) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}
