param(
    [string]$QdrantUrl = "http://localhost:6333",
    [string]$Collection = ("aura_compact_memory_poc_" + (Get-Date -Format "yyyyMMddHHmmss")),
    [string]$Query = "perche Aura e lenta quando crea documenti?",
    [int]$Dimensions = 64,
    [int]$TopK = 5,
    [switch]$KeepCollection
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Invoke-QdrantJson {
    param(
        [string]$Method,
        [string]$Path,
        [object]$Body = $null
    )

    $uri = $QdrantUrl.TrimEnd("/") + $Path
    $args = @{
        Method = $Method
        Uri = $uri
        TimeoutSec = 20
    }
    if ($null -ne $Body) {
        $args.ContentType = "application/json"
        $args.Body = ($Body | ConvertTo-Json -Depth 30 -Compress)
    }
    Invoke-RestMethod @args
}

function Get-Tokens {
    param([string]$Text)
    $out = New-Object System.Collections.Generic.List[string]
    foreach ($match in [regex]::Matches($Text.ToLowerInvariant(), "[\p{L}\p{Nd}_]+")) {
        if ($match.Value.Length -ge 2) {
            $out.Add($match.Value)
        }
    }
    $out
}

function Get-TokenIndex {
    param(
        [string]$Token,
        [int]$Size
    )
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($Token)
        $hash = $sha.ComputeHash($bytes)
        ([BitConverter]::ToUInt32($hash, 0) % $Size)
    }
    finally {
        $sha.Dispose()
    }
}

function ConvertTo-Vector {
    param(
        [string]$Text,
        [int]$Size
    )
    $vector = New-Object double[] $Size
    foreach ($token in Get-Tokens $Text) {
        $idx = Get-TokenIndex $token $Size
        $vector[$idx] += 1.0
    }
    $norm = 0.0
    foreach ($value in $vector) {
        $norm += ($value * $value)
    }
    if ($norm -le 0) {
        return $vector
    }
    $norm = [Math]::Sqrt($norm)
    for ($i = 0; $i -lt $vector.Length; $i++) {
        $vector[$i] = [Math]::Round($vector[$i] / $norm, 6)
    }
    $vector
}

function New-CompactMemoryFixture {
    @(
        [ordered]@{
            point_id = 1
            id = "fact:document-loop-62s"
            kind = "archive_fact"
            title = "Document route was slow before Runtime Diet"
            body_compact = "A broad document prompt previously took about 62 seconds, five loop steps, and excessive context before create_docx."
            handle = "conversation:document-route-smoke"
            entities = @("performance", "document-route", "agent-loop", "create_docx")
            tags = @("latency", "runtime-diet", "telegram")
            edges = @(
                @{ to = "entity:agent-loop"; relation = "caused_by"; weight = 0.90 },
                @{ to = "entity:document-route"; relation = "belongs_to"; weight = 0.95 },
                @{ to = "decision:runtime-diet"; relation = "motivated"; weight = 0.85 }
            )
        }
        [ordered]@{
            point_id = 2
            id = "decision:runtime-diet"
            kind = "decision"
            title = "Runtime Diet trims hot-path ceremony"
            body_compact = "If a subsystem does not help answer the current turn, it must move out of the hot path or be deleted."
            handle = "[[runtime-diet]]"
            entities = @("runtime-diet", "performance", "agent-loop")
            tags = @("decision", "architecture")
            edges = @(
                @{ to = "entity:performance"; relation = "improves"; weight = 0.75 },
                @{ to = "entity:agent-loop"; relation = "simplifies"; weight = 0.70 }
            )
        }
        [ordered]@{
            point_id = 3
            id = "entity:agent-loop"
            kind = "entity_node"
            title = "Agent loop"
            body_compact = "The model/tool loop should expose only relevant tools, reject duplicates, and finish from compact evidence."
            handle = "entity:agent-loop"
            entities = @("agent-loop", "toolset", "runner")
            tags = @("graph-node", "runtime")
            edges = @(
                @{ to = "decision:runtime-diet"; relation = "governed_by"; weight = 0.70 }
            )
        }
        [ordered]@{
            point_id = 4
            id = "entity:document-route"
            kind = "entity_node"
            title = "Document route"
            body_compact = "Document generation should use a retrieval capsule when present and then exactly one typed file tool."
            handle = "entity:document-route"
            entities = @("document-route", "create_docx", "retrieval-capsule")
            tags = @("graph-node", "document")
            edges = @(
                @{ to = "fact:document-hot-path-cut"; relation = "fixed_by"; weight = 0.95 }
            )
        }
        [ordered]@{
            point_id = 5
            id = "fact:document-hot-path-cut"
            kind = "wiki_fact"
            title = "Document hot path now skips redundant retrieval"
            body_compact = "The live document smoke now completes with one LLM call, one create_docx call, one loop step, and no hidden read_file."
            handle = "[[runtime-diet#document-hot-path-cut]]"
            entities = @("document-route", "retrieval-capsule", "create_docx", "performance")
            tags = @("verified", "smoke-test")
            edges = @(
                @{ to = "entity:document-route"; relation = "updates"; weight = 0.95 },
                @{ to = "entity:performance"; relation = "improves"; weight = 0.80 }
            )
        }
        [ordered]@{
            point_id = 6
            id = "entity:performance"
            kind = "entity_node"
            title = "Performance"
            body_compact = "Aura performance is dominated by model round trips, tool loops, and broad evidence expansion."
            handle = "entity:performance"
            entities = @("performance", "latency", "tool-calls")
            tags = @("graph-node")
            edges = @(
                @{ to = "fact:document-loop-62s"; relation = "evidenced_by"; weight = 0.85 },
                @{ to = "fact:document-hot-path-cut"; relation = "evidenced_by"; weight = 0.80 }
            )
        }
        [ordered]@{
            point_id = 7
            id = "proposal:compact-memory-index"
            kind = "proposal_fact"
            title = "Compact memory index proposal"
            body_compact = "Index source facts, archive facts, proposal facts, and graph nodes as compact Qdrant points; keep raw records behind handles."
            handle = "proposal:compact-memory-index"
            entities = @("compact-memory", "qdrant", "graph", "source", "archive")
            tags = @("proposal", "retrieval")
            edges = @(
                @{ to = "entity:performance"; relation = "improves"; weight = 0.75 },
                @{ to = "decision:runtime-diet"; relation = "implements"; weight = 0.80 }
            )
        }
    )
}

function Get-LexicalRanking {
    param(
        [array]$Items,
        [string]$InputQuery
    )
    $terms = @(Get-Tokens $InputQuery)
    $ranked = foreach ($item in $Items) {
        $text = (($item.title + " " + $item.body_compact + " " + (($item.entities + $item.tags) -join " "))).ToLowerInvariant()
        $score = 0.0
        foreach ($term in $terms) {
            $count = ([regex]::Matches($text, [regex]::Escape($term))).Count
            if ($count -gt 0) {
                $score += [Math]::Min($count, 4)
            }
            if ($item.title.ToLowerInvariant().Contains($term)) {
                $score += 1.5
            }
        }
        if ($score -gt 0) {
            [pscustomobject]@{ id = $item.id; score = $score; payload = $item }
        }
    }
    $ranked | Sort-Object -Property score -Descending
}

function Add-Rrf {
    param(
        [hashtable]$Scores,
        [hashtable]$Payloads,
        [hashtable]$Channels,
        [array]$Ranked,
        [string]$Channel,
        [double]$Weight = 1.0
    )
    for ($i = 0; $i -lt $Ranked.Count; $i++) {
        $row = $Ranked[$i]
        $id = [string]$row.id
        if (-not $Scores.ContainsKey($id)) {
            $Scores[$id] = 0.0
            $Channels[$id] = New-Object System.Collections.Generic.List[string]
        }
        $Scores[$id] = [double]$Scores[$id] + ($Weight * (1.0 / (60 + $i + 1)))
        $Payloads[$id] = $row.payload
        if (-not $Channels[$id].Contains($Channel)) {
            $Channels[$id].Add($Channel)
        }
    }
}

function Expand-Graph {
    param(
        [array]$Seeds,
        [array]$Items,
        [hashtable]$Scores,
        [hashtable]$Payloads,
        [hashtable]$Channels
    )
    $byID = @{}
    foreach ($item in $Items) {
        $byID[$item.id] = $item
    }
    foreach ($seed in ($Seeds | Select-Object -First 3)) {
        foreach ($edge in @($seed.payload.edges)) {
            $targetID = [string]$edge.to
            if (-not $byID.ContainsKey($targetID)) {
                continue
            }
            if (-not $Scores.ContainsKey($targetID)) {
                $Scores[$targetID] = 0.0
                $Channels[$targetID] = New-Object System.Collections.Generic.List[string]
            }
            $Scores[$targetID] = [double]$Scores[$targetID] + ([double]$seed.score * [double]$edge.weight * 0.35)
            $Payloads[$targetID] = $byID[$targetID]
            if (-not $Channels[$targetID].Contains("graph")) {
                $Channels[$targetID].Add("graph")
            }
        }
    }
}

function Format-Capsule {
    param(
        [array]$Results,
        [string]$InputQuery
    )
    $lines = New-Object System.Collections.Generic.List[string]
    $lines.Add("## Compact Memory Capsule (Qdrant PoC)")
    $lines.Add("")
    $lines.Add("Query: $InputQuery")
    $lines.Add("")
    $lines.Add("### Evidence")
    foreach ($result in ($Results | Select-Object -First $TopK)) {
        $p = $result.payload
        $snippet = $p.body_compact
        if ($snippet.Length -gt 180) {
            $snippet = $snippet.Substring(0, 177) + "..."
        }
        $channelText = ($result.channels -join ",")
        $lines.Add(("- {0} [{1}] score={2:n4} via={3} handle={4}" -f $p.id, $p.kind, $result.score, $channelText, $p.handle))
        $lines.Add("  $snippet")
    }
    $lines -join [Environment]::NewLine
}

try {
    Write-Host "Checking Qdrant at $QdrantUrl ..."
    Invoke-QdrantJson -Method GET -Path "/collections" | Out-Null

    $items = @(New-CompactMemoryFixture)
    Write-Host "Creating temporary collection $Collection ..."
    try {
        Invoke-QdrantJson -Method DELETE -Path "/collections/$Collection" | Out-Null
    }
    catch {
        # It is fine if the temporary collection does not exist yet.
    }
    Invoke-QdrantJson -Method PUT -Path "/collections/$Collection" -Body @{
        vectors = @{
            size = $Dimensions
            distance = "Cosine"
        }
    } | Out-Null

    $points = @()
    foreach ($item in $items) {
        $text = $item.title + " " + $item.body_compact + " " + (($item.entities + $item.tags) -join " ")
        $points += @{
            id = $item.point_id
            vector = @(ConvertTo-Vector -Text $text -Size $Dimensions)
            payload = $item
        }
    }
    Invoke-QdrantJson -Method PUT -Path "/collections/$Collection/points?wait=true" -Body @{ points = $points } | Out-Null

    $queryVector = @(ConvertTo-Vector -Text $Query -Size $Dimensions)
    $vectorResponse = Invoke-QdrantJson -Method POST -Path "/collections/$Collection/points/search" -Body @{
        vector = $queryVector
        limit = [Math]::Max($TopK * 3, 10)
        with_payload = $true
    }
    $vectorRanked = @($vectorResponse.result | ForEach-Object {
        [pscustomobject]@{ id = $_.payload.id; score = $_.score; payload = $_.payload }
    })
    $lexicalRanked = @(Get-LexicalRanking -Items $items -InputQuery $Query)

    $scores = @{}
    $payloads = @{}
    $channels = @{}
    Add-Rrf -Scores $scores -Payloads $payloads -Channels $channels -Ranked $vectorRanked -Channel "qdrant-vector" -Weight 0.65
    Add-Rrf -Scores $scores -Payloads $payloads -Channels $channels -Ranked $lexicalRanked -Channel "local-lexical" -Weight 0.35

    $seedResults = @($scores.Keys | ForEach-Object {
        [pscustomobject]@{ id = $_; score = [double]$scores[$_]; payload = $payloads[$_]; channels = @($channels[$_]) }
    } | Sort-Object -Property score -Descending)
    Expand-Graph -Seeds $seedResults -Items $items -Scores $scores -Payloads $payloads -Channels $channels

    $results = @($scores.Keys | ForEach-Object {
        [pscustomobject]@{ id = $_; score = [double]$scores[$_]; payload = $payloads[$_]; channels = @($channels[$_]) }
    } | Sort-Object -Property score -Descending)

    Write-Host ""
    Write-Host (Format-Capsule -Results $results -InputQuery $Query)
    Write-Host ""

    $topIDs = @($results | Select-Object -First $TopK | ForEach-Object { $_.id })
    if ($topIDs -notcontains "fact:document-loop-62s") {
        throw "Self-test failed: expected fact:document-loop-62s in top $TopK, got: $($topIDs -join ', ')"
    }
    if (($results.id) -notcontains "entity:agent-loop") {
        throw "Self-test failed: expected graph expansion to include entity:agent-loop"
    }

    Write-Host "PASS: Qdrant compact-memory PoC returned compact facts plus graph-expanded nodes."
}
finally {
    if (-not $KeepCollection) {
        try {
            Invoke-QdrantJson -Method DELETE -Path "/collections/$Collection" | Out-Null
            Write-Host "Deleted temporary collection $Collection."
        }
        catch {
            Write-Warning "Failed to delete temporary collection ${Collection}: $($_.Exception.Message)"
        }
    }
}
