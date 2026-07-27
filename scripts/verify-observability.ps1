[CmdletBinding()]
param(
    [switch]$StaticOnly,
    [string]$RepositoryRoot = '',
    [switch]$SkipContainerTools
)

$ErrorActionPreference = 'Stop'
if ([string]::IsNullOrWhiteSpace($RepositoryRoot)) {
    $RepositoryRoot = Split-Path -Parent $PSScriptRoot
}
$repoRoot = (Resolve-Path -LiteralPath $RepositoryRoot).Path

function Assert-Observability {
    param(
        [Parameter(Mandatory)] [bool]$Condition,
        [Parameter(Mandatory)] [string]$Message
    )
    if (-not $Condition) {
        throw "observability verification failed: $Message"
    }
}

function Invoke-CheckedTool {
    param(
        [Parameter(Mandatory)] [string]$Label,
        [Parameter(Mandatory)] [string]$Command,
        [Parameter(Mandatory)] [string[]]$Arguments
    )

    $savedErrorPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'SilentlyContinue'
        $output = & $Command @Arguments 2>&1
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $savedErrorPreference
    }
    if ($exitCode -ne 0) {
        throw "observability verification failed: $Label failed: $($output | Out-String)"
    }
    return ($output | Out-String).Trim()
}

function ConvertTo-NativeDottedArgument {
    param([Parameter(Mandatory)] [string]$Value)

    if ($env:OS -eq 'Windows_NT' -and $PSVersionTable.PSVersion.Major -lt 7) {
        return '"' + $Value + '"'
    }
    return $Value
}

function Assert-SimpleYaml {
    param([Parameter(Mandatory)] [string]$Path)

    $previousIndent = 0
    $previousOpenedBlock = $true
    $lineNumber = 0
    foreach ($line in Get-Content -LiteralPath $Path) {
        $lineNumber++
        if ($line.Contains("`t")) {
            throw "observability verification failed: invalid YAML in $Path line ${lineNumber}: tabs are forbidden"
        }
        $trimmed = $line.Trim()
        if ($trimmed.Length -eq 0 -or $trimmed.StartsWith('#')) {
            continue
        }
        $indent = $line.Length - $line.TrimStart().Length
        if (($indent % 2) -ne 0 -or ($indent -gt $previousIndent -and -not $previousOpenedBlock)) {
            throw "observability verification failed: invalid YAML in $Path line ${lineNumber}: invalid indentation"
        }
        if ($trimmed -notmatch '^(?:-\s+(?:[A-Za-z0-9_.-]+:\s*.*|\S.*)|[A-Za-z0-9_.-]+:\s*.*)$') {
            throw "observability verification failed: invalid YAML in $Path line ${lineNumber}: malformed mapping or sequence"
        }

        $stack = [System.Collections.Generic.Stack[char]]::new()
        $quote = [char]0
        $escaped = $false
        foreach ($character in $trimmed.ToCharArray()) {
            if ($escaped) {
                $escaped = $false
                continue
            }
            if ($quote -ne [char]0) {
                if ($character -eq '\\' -and $quote -eq '"') {
                    $escaped = $true
                } elseif ($character -eq $quote) {
                    $quote = [char]0
                }
                continue
            }
            if ($character -eq '"' -or $character -eq "'") {
                $quote = $character
            } elseif ($character -eq '[' -or $character -eq '{') {
                $stack.Push($character)
            } elseif ($character -eq ']' -or $character -eq '}') {
                if ($stack.Count -eq 0) {
                    throw "observability verification failed: invalid YAML in $Path line ${lineNumber}: unmatched flow delimiter"
                }
                $opening = $stack.Pop()
                if (($opening -eq '[' -and $character -ne ']') -or ($opening -eq '{' -and $character -ne '}')) {
                    throw "observability verification failed: invalid YAML in $Path line ${lineNumber}: mismatched flow delimiter"
                }
            }
        }
        if ($quote -ne [char]0 -or $stack.Count -ne 0) {
            throw "observability verification failed: invalid YAML in $Path line ${lineNumber}: unterminated flow value"
        }
        $previousIndent = $indent
        $previousOpenedBlock = $trimmed.EndsWith(':') -or $trimmed -match '^-\s+[A-Za-z0-9_.-]+:'
    }
}

function Get-ComposeServiceBlock {
    param(
        [Parameter(Mandatory)] [string]$Compose,
        [Parameter(Mandatory)] [string]$Name
    )

    $escaped = [regex]::Escape($Name)
    $match = [regex]::Match($Compose, "(?ms)^  ${escaped}:\r?\n.*?(?=^  [A-Za-z0-9_-]+:\r?\n|^volumes:\r?\n)")
    Assert-Observability $match.Success "compose service $Name is missing"
    return $match.Value
}

function Get-YamlValue {
    param(
        [Parameter(Mandatory)] [string]$Body,
        [Parameter(Mandatory)] [string]$Key
    )

    $match = [regex]::Match($Body, "(?m)^\s+$([regex]::Escape($Key)):\s*(?<value>.+?)\s*$")
    Assert-Observability $match.Success "alert is missing $Key"
    return $match.Groups['value'].Value.Trim('"', "'")
}

function Get-KnownMetricContract {
    param([Parameter(Mandatory)] [string]$CatalogPath)

    $catalog = Get-Content -LiteralPath $CatalogPath -Raw
    $known = @{}
    foreach ($match in [regex]::Matches($catalog, '"(?<metric>aura_[a-z0-9_]+)"')) {
        $known[$match.Groups['metric'].Value] = $true
    }
    foreach ($line in $catalog -split "`r?`n") {
        if ($line -match 'histogram\(.*"(?<metric>aura_[a-z0-9_]+)"') {
            foreach ($suffix in @('_bucket', '_count', '_sum')) {
                $known[$Matches.metric + $suffix] = $true
            }
        }
    }
    $recording = Get-Content -LiteralPath (Join-Path $repoRoot 'observability/prometheus/rules/aura-recording.yml') -Raw
    foreach ($match in [regex]::Matches($recording, '(?m)^\s*-\s+record:\s+(?<metric>\S+)\s*$')) {
        $known[$match.Groups['metric'].Value] = $true
    }
    $known['up'] = $true
    return $known
}

function Assert-QueryContract {
    param(
        [Parameter(Mandatory)] [string]$Query,
        [Parameter(Mandatory)] [hashtable]$KnownMetrics,
        [Parameter(Mandatory)] [string]$Source
    )

    foreach ($match in [regex]::Matches($Query, '(?<![A-Za-z0-9_])(?<metric>aura(?::|_)[A-Za-z0-9_:]*[A-Za-z0-9_])')) {
        $metric = $match.Groups['metric'].Value
        Assert-Observability $KnownMetrics.ContainsKey($metric) "unknown metric $metric in $Source"
    }
    $allowedLabels = @{
        operation = $true; tool_class = $true; transport = $true; outcome = $true
        error_class = $true; state = $true; le = $true; job = $true; instance = $true
    }
    foreach ($selector in [regex]::Matches($Query, '(?<metric>(?:aura(?::|_)[A-Za-z0-9_:]+|up))\{(?<labels>[^}]*)\}')) {
        foreach ($label in [regex]::Matches($selector.Groups['labels'].Value, '(?<key>[A-Za-z_][A-Za-z0-9_]*)\s*(?:!?=|=~|!~)')) {
            $key = $label.Groups['key'].Value
            Assert-Observability $allowedLabels.ContainsKey($key) "forbidden label $key in $Source"
        }
    }
}

$dashboardContracts = [ordered]@{
    'aura-overview.json'       = 'aura-overview'
    'aura-agents.json'         = 'aura-agents'
    'aura-tools-mcp.json'      = 'aura-tools-mcp'
    'aura-data-retention.json' = 'aura-data-retention'
}
$expectedThresholds = @{
    'aura-overview/1' = @(1); 'aura-overview/2' = @(0); 'aura-overview/3' = @(1)
    'aura-overview/4' = @(0); 'aura-overview/6' = @(0.05); 'aura-overview/7' = @(50)
    'aura-agents/2' = @(14.4); 'aura-agents/3' = @(14.4)
    'aura-tools-mcp/2' = @(0.1); 'aura-tools-mcp/3' = @(0.1); 'aura-tools-mcp/4' = @(5)
    'aura-tools-mcp/6' = @(0); 'aura-tools-mcp/7' = @(20); 'aura-tools-mcp/8' = @(0)
    'aura-data-retention/3' = @(0); 'aura-data-retention/4' = @(100)
    'aura-data-retention/5' = @(0.7, 0.8, 0.85); 'aura-data-retention/7' = @(10000)
}

$dashboardRoot = Join-Path $repoRoot 'observability/grafana/dashboards'
$dashboardsByUid = @{}
$seenUids = @{}
$queries = [System.Collections.Generic.List[object]]::new()
foreach ($entry in $dashboardContracts.GetEnumerator()) {
    $path = Join-Path $dashboardRoot $entry.Key
    Assert-Observability (Test-Path -LiteralPath $path -PathType Leaf) "missing dashboard $($entry.Key)"
    try {
        $dashboard = Get-Content -LiteralPath $path -Raw | ConvertFrom-Json
    } catch {
        throw "observability verification failed: invalid dashboard JSON $($entry.Key): $($_.Exception.Message)"
    }
    Assert-Observability (-not $seenUids.ContainsKey($dashboard.uid)) "duplicate dashboard UID $($dashboard.uid)"
    Assert-Observability ($dashboard.uid -ceq $entry.Value) "dashboard $($entry.Key) must use UID $($entry.Value)"
    $seenUids[$dashboard.uid] = $true
    $dashboardsByUid[$dashboard.uid] = $dashboard
    $panelIds = @($dashboard.panels | ForEach-Object { [int]$_.id })
    Assert-Observability ($panelIds.Count -gt 0) "dashboard $($entry.Key) has no panels"
    Assert-Observability (($panelIds | Select-Object -Unique).Count -eq $panelIds.Count) "dashboard $($entry.Key) has duplicate panel IDs"
    Assert-Observability (@($dashboard.templating.list).Count -eq 0) "dashboard $($entry.Key) must not expose unbounded variables"
    foreach ($panel in @($dashboard.panels)) {
        Assert-Observability ($panel.description -and $panel.fieldConfig.defaults.noValue -eq 'No data') "panel $($dashboard.uid)/$($panel.id) must document no-data behavior"
        foreach ($target in @($panel.targets)) {
            Assert-Observability ($panel.datasource.uid -eq 'aura-prometheus') "panel $($dashboard.uid)/$($panel.id) must use aura-prometheus"
            $queries.Add([pscustomobject]@{ Query = [string]$target.expr; Source = "$($dashboard.uid)/$($panel.id)" })
        }
        $thresholdKey = "$($dashboard.uid)/$($panel.id)"
        if ($expectedThresholds.ContainsKey($thresholdKey)) {
            $actual = @($panel.fieldConfig.defaults.thresholds.steps | Where-Object { $null -ne $_.value } | ForEach-Object { [double]$_.value })
            foreach ($expected in $expectedThresholds[$thresholdKey]) {
                Assert-Observability ($actual -contains [double]$expected) "panel $thresholdKey threshold drift: missing $expected"
            }
        }
    }
}

$retentionBacklogPanel = @($dashboardsByUid['aura-data-retention'].panels | Where-Object { [int]$_.id -eq 4 })
Assert-Observability ($retentionBacklogPanel.Count -eq 1) 'retention backlog current-state panel is missing'
$retentionBacklogTargets = @($retentionBacklogPanel[0].targets)
Assert-Observability ($retentionBacklogTargets.Count -eq 1) 'retention backlog panel must have exactly one query'
Assert-Observability (
    $retentionBacklogTargets[0].instant -eq $true -and $retentionBacklogTargets[0].range -eq $false
) 'retention backlog panel must query the current instant so missing observations render No data'

$yamlPaths = @(
    'observability/grafana/provisioning/datasources/aura.yml',
    'observability/grafana/provisioning/dashboards/aura.yml',
    'observability/tempo/tempo.yml',
    'observability/prometheus/prometheus.yml',
    'observability/prometheus/rules/aura-recording.yml',
    'observability/prometheus/rules/aura-alerts.yml',
    'observability/prometheus/tests/aura-rules.test.yml'
)
foreach ($relative in $yamlPaths) {
    $path = Join-Path $repoRoot $relative
    Assert-Observability (Test-Path -LiteralPath $path -PathType Leaf) "missing $relative"
    Assert-SimpleYaml -Path $path
}

$datasourceText = Get-Content -LiteralPath (Join-Path $repoRoot $yamlPaths[0]) -Raw
$providerText = Get-Content -LiteralPath (Join-Path $repoRoot $yamlPaths[1]) -Raw
$tempoText = Get-Content -LiteralPath (Join-Path $repoRoot $yamlPaths[2]) -Raw
Assert-Observability ($datasourceText -cmatch 'uid:\s+aura-prometheus') 'Prometheus datasource UID is not stable'
Assert-Observability ($datasourceText -cmatch 'uid:\s+aura-tempo') 'Tempo datasource UID is not stable'
Assert-Observability ($datasourceText -cmatch 'exemplarTraceIdDestinations:') 'Prometheus datasource lacks trace exemplar drilldown'
Assert-Observability ($datasourceText -cmatch 'tracesToMetrics:') 'Tempo datasource lacks trace-to-metrics drilldown'
Assert-Observability ($providerText -cmatch 'allowUiUpdates:\s+false') 'provisioned dashboards must be read-only'
Assert-Observability ($tempoText -cmatch 'block_retention:\s+336h') 'Tempo must own a bounded 14-day retention window'
Assert-Observability ($tempoText -cmatch 'backend:\s+local') 'Tempo storage must stay local and bounded'

foreach ($relative in @('observability/prometheus/rules/aura-recording.yml', 'observability/prometheus/rules/aura-alerts.yml', 'observability/prometheus/tests/aura-rules.test.yml')) {
    $text = Get-Content -LiteralPath (Join-Path $repoRoot $relative) -Raw
    foreach ($match in [regex]::Matches($text, '(?m)^\s*-?\s*expr:\s*(?<query>.+?)\s*$')) {
        $queries.Add([pscustomobject]@{ Query = $match.Groups['query'].Value; Source = $relative })
    }
}
$knownMetrics = Get-KnownMetricContract -CatalogPath (Join-Path $repoRoot 'internal/obs/catalog.go')
foreach ($query in $queries) {
    Assert-QueryContract -Query $query.Query -KnownMetrics $knownMetrics -Source $query.Source
}

$alertsText = Get-Content -LiteralPath (Join-Path $repoRoot 'observability/prometheus/rules/aura-alerts.yml') -Raw
$alerts = [regex]::Matches($alertsText, '(?ms)^      - alert:\s*(?<name>\S+)\r?\n(?<body>.*?)(?=^      - alert:|^  - name:|\z)')
Assert-Observability ($alerts.Count -gt 0) 'no alert link contracts found'
foreach ($alert in $alerts) {
    $name = $alert.Groups['name'].Value
    $body = $alert.Groups['body'].Value
    $uid = Get-YamlValue $body 'dashboard_uid'
    $panelId = [int](Get-YamlValue $body 'panel_id')
    $dashboardUrl = Get-YamlValue $body 'dashboard_url'
    $runbook = Get-YamlValue $body 'runbook_url'
    $threshold = Get-YamlValue $body 'threshold'
    Assert-Observability $dashboardsByUid.ContainsKey($uid) "alert $name references missing dashboard $uid"
    $panel = @($dashboardsByUid[$uid].panels | Where-Object { [int]$_.id -eq $panelId })
    Assert-Observability ($panel.Count -eq 1) "alert $name references missing panel $uid/$panelId"
    Assert-Observability ($dashboardUrl -eq "/d/${uid}?viewPanel=${panelId}") "alert $name dashboard URL drift"
    Assert-Observability (Test-Path -LiteralPath (Join-Path $repoRoot $runbook) -PathType Leaf) "alert $name references missing runbook $runbook"
    $panelLinks = @($panel[0].fieldConfig.defaults.links | ForEach-Object { [string]$_.url })
    Assert-Observability (@($panelLinks | Where-Object { $_.Contains($runbook) }).Count -gt 0) "alert $name panel lacks runbook link $runbook"
    foreach ($number in [regex]::Matches($threshold, '\d+(?:\.\d+)?')) {
        Assert-Observability ($panel[0].description.Contains($number.Value)) "alert $name threshold annotation drift at $($number.Value)"
    }
}

$composePath = Join-Path $repoRoot 'compose.yaml'
$composeText = Get-Content -LiteralPath $composePath -Raw
$prometheusBlock = Get-ComposeServiceBlock $composeText 'prometheus'
$tempoBlock = Get-ComposeServiceBlock $composeText 'tempo'
$grafanaBlock = Get-ComposeServiceBlock $composeText 'grafana'
$imageContracts = @(
    @{ Name = 'Prometheus'; Block = $prometheusBlock; Prefix = 'prom/prometheus:v3.13.1' },
    @{ Name = 'Tempo'; Block = $tempoBlock; Prefix = 'grafana/tempo:2.9.4' },
    @{ Name = 'Grafana'; Block = $grafanaBlock; Prefix = 'grafana/grafana:12.3.9' }
)
foreach ($contract in $imageContracts) {
    $imageMatch = [regex]::Match($contract.Block, '(?m)^    image:\s+(?<image>\S+)\s*$')
    Assert-Observability ($imageMatch.Success -and $imageMatch.Groups['image'].Value -match "^$([regex]::Escape($contract.Prefix))@sha256:[0-9a-f]{64}$") "$($contract.Name) image must be digest-pinned"
}
Assert-Observability ($prometheusBlock -match 'profiles:\s+\[observability\]') 'Prometheus must be profile-gated'
Assert-Observability ($tempoBlock -match 'profiles:\s+\[observability\]') 'Tempo must be profile-gated'
Assert-Observability ($grafanaBlock -match 'profiles:\s+\[observability\]') 'Grafana must be profile-gated'
Assert-Observability ($prometheusBlock -match 'network_mode:\s+"service:aura"') 'Prometheus must share Aura private network namespace'
Assert-Observability ($tempoBlock -match 'network_mode:\s+"service:aura"') 'Tempo must share Aura private network namespace'
Assert-Observability ($prometheusBlock -notmatch '(?m)^    ports:') 'Prometheus scrape port 9090 must not be host-published'
Assert-Observability ($tempoBlock -notmatch '(?m)^    ports:') 'Tempo ingestion port 4317 must not be host-published'
Assert-Observability ($grafanaBlock -match '127\.0\.0\.1:\$\{AURA_GRAFANA_PORT:-3000\}:3000') 'Grafana must publish only on host loopback'
Assert-Observability ($prometheusBlock -match 'prometheus\.yml:/etc/prometheus/prometheus\.yml:ro') 'Prometheus config mount must be read-only'
Assert-Observability ($tempoBlock -match 'tempo\.yml:/etc/tempo/tempo\.yml:ro') 'Tempo config mount must be read-only'
Assert-Observability ($grafanaBlock -match 'provisioning:/etc/grafana/provisioning:ro') 'Grafana provisioning mount must be read-only'
$providerPathMatch = [regex]::Match($providerText, '(?m)^\s+path:\s+(?<path>\S+)\s*$')
Assert-Observability ($providerPathMatch.Success -and $grafanaBlock.Contains(":" + $providerPathMatch.Groups['path'].Value + ':ro')) 'dashboard provider path does not match the Grafana mount'
foreach ($block in @($prometheusBlock, $tempoBlock, $grafanaBlock)) {
    Assert-Observability ($block -match '(?m)^    mem_limit:') 'observability service lacks a memory limit'
    Assert-Observability ($block -match '(?m)^    cpus:') 'observability service lacks a CPU limit'
    Assert-Observability ($block -match '(?m)^    pids_limit:') 'observability service lacks a PID limit'
    Assert-Observability ($block -match '(?m)^    healthcheck:') 'observability service lacks a healthcheck'
}

$ciPath = Join-Path $repoRoot '.github/workflows/ci.yml'
$ciText = Get-Content -LiteralPath $ciPath -Raw
Assert-Observability ($ciText.Contains('pwsh -NoProfile -File scripts/verify-observability.ps1 -StaticOnly')) 'CI must run the same observability verifier entry point'
Assert-Observability ($ciText.Contains('pwsh -NoProfile -File scripts/verify-observability.Tests.ps1')) 'CI must run observability negative fixtures'

if (-not $SkipContainerTools) {
    Assert-Observability ($null -ne (Get-Command docker -ErrorAction SilentlyContinue)) 'Docker is required for hermetic validation'
    $prometheusImage = ([regex]::Match($prometheusBlock, '(?m)^    image:\s+(?<image>\S+)\s*$')).Groups['image'].Value
    $tempoImage = ([regex]::Match($tempoBlock, '(?m)^    image:\s+(?<image>\S+)\s*$')).Groups['image'].Value
    $grafanaImage = ([regex]::Match($grafanaBlock, '(?m)^    image:\s+(?<image>\S+)\s*$')).Groups['image'].Value
    $prometheusRoot = Join-Path $repoRoot 'observability/prometheus'
    $tempoRoot = Join-Path $repoRoot 'observability/tempo'
    Invoke-CheckedTool 'promtool config validation' docker @('run', '--rm', '--entrypoint', '/bin/promtool', '--volume', "${prometheusRoot}:/etc/prometheus:ro", $prometheusImage, 'check', 'config', '/etc/prometheus/prometheus.yml') | Out-Null
    Invoke-CheckedTool 'promtool rule tests' docker @('run', '--rm', '--entrypoint', '/bin/promtool', '--volume', "${prometheusRoot}:/etc/prometheus:ro", $prometheusImage, 'test', 'rules', '/etc/prometheus/tests/aura-rules.test.yml') | Out-Null
    $tempoConfigArgument = ConvertTo-NativeDottedArgument '-config.file=/etc/tempo/tempo.yml'
    $tempoVerifyArgument = ConvertTo-NativeDottedArgument '-config.verify=true'
    Invoke-CheckedTool 'Tempo config validation' docker @('run', '--rm', '--volume', "${tempoRoot}:/etc/tempo:ro", $tempoImage, $tempoConfigArgument, $tempoVerifyArgument) | Out-Null
    Invoke-CheckedTool 'Grafana image validation' docker @('run', '--rm', '--entrypoint', '/usr/share/grafana/bin/grafana', $grafanaImage, '--version') | Out-Null

    $requiredComposeEnv = @('AURA_ACCESS_TOKEN', 'AURA_AUTHULA_SECRET', 'AURA_GARAGE_ADMIN_TOKEN', 'AURA_OBJECTSTORE_ACCESS_KEY', 'AURA_OBJECTSTORE_SECRET_KEY', 'AURA_PIM_MCP_ADMIN_TOKEN', 'GARAGE_RPC_SECRET', 'NEO4J_PASSWORD', 'POSTGRES_PASSWORD', 'SEARXNG_SECRET')
    $restoreEnv = @{}
    try {
        foreach ($name in $requiredComposeEnv) {
            $restoreEnv[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
            if ([string]::IsNullOrWhiteSpace($restoreEnv[$name])) {
                [Environment]::SetEnvironmentVariable($name, 'observability-verifier-placeholder', 'Process')
            }
        }
        Invoke-CheckedTool 'Compose observability profile validation' docker @('compose', '--project-directory', $repoRoot, '--profile', 'observability', '-f', $composePath, 'config', '--quiet') | Out-Null
    } finally {
        foreach ($name in $requiredComposeEnv) {
            [Environment]::SetEnvironmentVariable($name, $restoreEnv[$name], 'Process')
        }
    }
}

Write-Host "observability static verification passed: $($dashboardContracts.Count) dashboards, $($alerts.Count) alerts, $($queries.Count) checked queries"

function Wait-SmokeEndpoint {
    param(
        [Parameter(Mandatory)] [string]$Container,
        [Parameter(Mandatory)] [string]$Url,
        [Parameter(Mandatory)] [string]$Expected
    )

    for ($attempt = 0; $attempt -lt 45; $attempt++) {
        $savedErrorPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'SilentlyContinue'
            $output = & docker exec $Container /bin/wget -qO- -T 2 $Url 2>$null
            $exitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $savedErrorPreference
        }
        $response = ($output | Out-String)
        if ($exitCode -eq 0 -and $response.IndexOf($Expected, [System.StringComparison]::OrdinalIgnoreCase) -ge 0) {
            return $response
        }
        Start-Sleep -Seconds 1
    }
    $detail = if ([string]::IsNullOrWhiteSpace($response)) { '<empty response>' } else { $response.Trim() }
    throw "observability verification failed: runtime endpoint did not become ready: $Url; last response: $detail"
}

function Invoke-ObservabilitySmoke {
    $suffix = [guid]::NewGuid().ToString('N').Substring(0, 10)
    $network = "aura-observability-${suffix}"
    $keeper = "aura-smoke-${suffix}"
    $prometheus = "aura-prometheus-smoke-${suffix}"
    $tempo = "aura-tempo-smoke-${suffix}"
    $grafana = "aura-grafana-smoke-${suffix}"
    $smokeRoot = Join-Path ([System.IO.Path]::GetTempPath()) $network
    $prometheusImage = ([regex]::Match($prometheusBlock, '(?m)^    image:\s+(?<image>\S+)\s*$')).Groups['image'].Value
    $tempoImage = ([regex]::Match($tempoBlock, '(?m)^    image:\s+(?<image>\S+)\s*$')).Groups['image'].Value
    $grafanaImage = ([regex]::Match($grafanaBlock, '(?m)^    image:\s+(?<image>\S+)\s*$')).Groups['image'].Value
    try {
        New-Item -ItemType Directory -Path $smokeRoot -Force | Out-Null
        $metric = "# TYPE aura_agent_turn_total counter`naura_agent_turn_total{outcome=`"success`"} 1`n"
        [System.IO.File]::WriteAllText((Join-Path $smokeRoot 'metrics'), $metric, [System.Text.UTF8Encoding]::new($false))
        $traceHex = '0123456789abcdef0123456789abcdef'
        $spanHex = $traceHex.Substring(0, 16)
        $start = [long]([DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() * 1000000)
        $end = $start + 1000000
        $startText = $start.ToString([System.Globalization.CultureInfo]::InvariantCulture)
        $endText = $end.ToString([System.Globalization.CultureInfo]::InvariantCulture)
        $trace = '{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"aura-smoke"}}]},"scopeSpans":[{"scope":{"name":"aura-verifier"},"spans":[{"traceId":"' + $traceHex + '","spanId":"' + $spanHex + '","name":"observability-verification","kind":1,"startTimeUnixNano":"' + $startText + '","endTimeUnixNano":"' + $endText + '","status":{"code":1}}]}]}]}'
        [System.IO.File]::WriteAllText((Join-Path $smokeRoot 'trace.json'), $trace, [System.Text.UTF8Encoding]::new($false))

        Invoke-CheckedTool 'smoke network creation' docker @('network', 'create', $network) | Out-Null
        Invoke-CheckedTool 'synthetic Aura endpoint start' docker @('run', '-d', '--name', $keeper, '--network', $network, '--network-alias', 'aura', '--volume', "${smokeRoot}:/synthetic:ro", '--entrypoint', '/bin/busybox', $prometheusImage, 'httpd', '-f', '-p', '9464', '-h', '/synthetic') | Out-Null
        $tempoConfigArgument = ConvertTo-NativeDottedArgument '-config.file=/etc/tempo/tempo.yml'
        Invoke-CheckedTool 'Tempo smoke start' docker @('run', '-d', '--name', $tempo, '--network', "container:${keeper}", '--user', '0:0', '--volume', "$(Join-Path $repoRoot 'observability/tempo'):/etc/tempo:ro", '--tmpfs', '/var/tempo:rw', $tempoImage, $tempoConfigArgument) | Out-Null
        $prometheusConfigArgument = ConvertTo-NativeDottedArgument '--config.file=/etc/prometheus/prometheus.yml'
        $prometheusStorageArgument = ConvertTo-NativeDottedArgument '--storage.tsdb.path=/prometheus'
        Invoke-CheckedTool 'Prometheus smoke start' docker @('run', '-d', '--name', $prometheus, '--network', "container:${keeper}", '--user', '0:0', '--volume', "$(Join-Path $repoRoot 'observability/prometheus'):/etc/prometheus:ro", '--tmpfs', '/prometheus:rw', '--entrypoint', '/bin/prometheus', $prometheusImage, $prometheusConfigArgument, $prometheusStorageArgument) | Out-Null
        Invoke-CheckedTool 'Grafana smoke start' docker @('run', '-d', '--name', $grafana, '--network', $network, '--volume', "$(Join-Path $repoRoot 'observability/grafana/provisioning'):/etc/grafana/provisioning:ro", '--volume', "$(Join-Path $repoRoot 'observability/grafana/dashboards'):/var/lib/grafana/dashboards:ro", '--tmpfs', '/tmp/grafana-data:rw,uid=472,gid=0,mode=0755', '-e', 'GF_PATHS_DATA=/tmp/grafana-data', '-e', 'GF_PATHS_PLUGINS=/tmp/grafana-data/plugins', '-e', 'GF_AUTH_ANONYMOUS_ENABLED=true', '-e', 'GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer', '-e', 'GF_AUTH_DISABLE_LOGIN_FORM=true', '-e', 'GF_ANALYTICS_REPORTING_ENABLED=false', '-e', 'GF_ANALYTICS_CHECK_FOR_UPDATES=false', '-e', 'GF_PLUGINS_PREINSTALL_DISABLED=true', $grafanaImage) | Out-Null

        Wait-SmokeEndpoint $keeper 'http://127.0.0.1:3200/ready' 'ready' | Out-Null
        Wait-SmokeEndpoint $keeper 'http://127.0.0.1:9090/-/ready' 'ready' | Out-Null
        Wait-SmokeEndpoint $keeper 'http://127.0.0.1:9464/metrics' 'aura_agent_turn_total' | Out-Null
        Wait-SmokeEndpoint $keeper "http://${grafana}:3000/api/health" 'ok' | Out-Null
        Invoke-CheckedTool 'synthetic OTLP trace' docker @('exec', $keeper, '/bin/wget', '-qO-', '-T', '5', '--header', 'Content-Type: application/json', '--post-file', '/synthetic/trace.json', 'http://127.0.0.1:4318/v1/traces') | Out-Null
        Wait-SmokeEndpoint $keeper "http://127.0.0.1:3200/api/traces/${traceHex}" 'observability-verification' | Out-Null
        Wait-SmokeEndpoint $keeper 'http://127.0.0.1:9090/api/v1/query?query=up%7Bjob%3D%22aura%22%7D%20%3D%3D%201' '"job":"aura"' | Out-Null
        foreach ($uid in $dashboardContracts.Values) {
            Wait-SmokeEndpoint $keeper "http://${grafana}:3000/api/dashboards/uid/${uid}" $uid | Out-Null
        }
        Write-Host 'observability runtime smoke passed: synthetic bounded series, OTLP trace, and four provisioned dashboards'
    } finally {
        foreach ($container in @($grafana, $prometheus, $tempo, $keeper)) {
            & docker rm -f $container 2>$null | Out-Null
        }
        & docker network rm $network 2>$null | Out-Null
        if (Test-Path -LiteralPath $smokeRoot) {
            Remove-Item -LiteralPath $smokeRoot -Recurse -Force
        }
    }
}

if (-not $StaticOnly) {
    Assert-Observability (-not $SkipContainerTools) 'runtime smoke cannot skip container tooling'
    Invoke-ObservabilitySmoke
}
