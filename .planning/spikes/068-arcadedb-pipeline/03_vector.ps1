# Spike 068 - Stage C: ArcadeDB-native vector path (LSM_VECTOR + vectorNeighbors) over the
# HTTP/SQL API, with REAL Granite-97m 384d embeddings. ASCII-only; invariant-culture floats.
$ErrorActionPreference = "Stop"
$inv = [System.Globalization.CultureInfo]::InvariantCulture
$auth = @{ Authorization = "Basic " + [Convert]::ToBase64String([Text.Encoding]::ASCII.GetBytes("root:playwithdata")) }
$cmdUrl = "http://127.0.0.1:12480/api/v1/command/aura"
function Sql($q){ $b = @{ language="sql"; command=$q } | ConvertTo-Json; Invoke-RestMethod -Uri $cmdUrl -Method Post -Headers $auth -ContentType "application/json" -Body $b }
function Lit($vec){ "[" + (($vec | ForEach-Object { $_.ToString("G9",$inv) }) -join ",") + "]" }

$corpus = [ordered]@{ "e1"="Tesla Model 3 electric car"; "e2"="Davide the car owner"; "e4"="a red electric vehicle"; "e5"="the hungry cat sleeps" }
$queryText = "electric vehicle"
$inputs = @($corpus.Values) + @($queryText)
$body = @{ input = $inputs } | ConvertTo-Json
$r = Invoke-RestMethod -Uri "http://127.0.0.1:8081/v1/embeddings" -Method Post -ContentType "application/json" -Body $body -TimeoutSec 60

"=== create Doc type + ARRAY_OF_FLOATS property + LSM_VECTOR index (HNSW, cosine, 384d) ==="
try { Sql "CREATE VERTEX TYPE Doc" | Out-Null; "OK type" } catch { "type: " + $_.Exception.Message }
try { Sql "CREATE PROPERTY Doc.embedding ARRAY_OF_FLOATS" | Out-Null; "OK property" } catch { "prop: " + $_.Exception.Message }
try { Sql "CREATE INDEX ON Doc (embedding) LSM_VECTOR METADATA {dimensions: 384, similarity: 'COSINE', maxConnections: 16, beamWidth: 100}" | Out-Null; "OK index" } catch { "idx: " + $_.Exception.Message }

"=== insert 4 Doc records with real embeddings ==="
$i = 0
foreach ($k in $corpus.Keys){
  $vec = $r.data[$i].embedding; $txt = $corpus[$k].Replace("'","''")
  try { Sql "INSERT INTO Doc SET node_id = '$k', txt = '$txt', embedding = $(Lit $vec)" | Out-Null; "OK insert $k" } catch { "ins ${k}: " + $_.Exception.Message }
  $i++
}
$qv = Lit $r.data[$i].embedding

"=== vectorNeighbors() ANN search for query 'electric vehicle' (top 3) ==="
$res = Sql "SELECT node_id, txt, `$distance AS dist FROM (SELECT expand(vectorNeighbors('Doc[embedding]', $qv, 3)))"
$res.result | ForEach-Object { "{0}  {1}  dist={2}" -f $_.node_id, $_.txt, $_.dist }