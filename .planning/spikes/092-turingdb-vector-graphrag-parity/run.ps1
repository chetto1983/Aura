$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..\..")).Path
$ScratchRoot = if ($env:TURINGDB_SPIKE_HOME) { $env:TURINGDB_SPIKE_HOME } else { "D:\tmp\turingdb-aura-spike-docker" }
$Image = if ($env:TURINGDB_DOCKER_IMAGE) { $env:TURINGDB_DOCKER_IMAGE } else { "aura-turingdb-spike:1.35" }

New-Item -ItemType Directory -Force -Path $ScratchRoot | Out-Null
$ImageExists = $false
try {
  docker image inspect $Image *> $null
  if ($LASTEXITCODE -eq 0) { $ImageExists = $true }
} catch {
  $ImageExists = $false
}
if (-not $ImageExists) {
  docker build -t $Image -f (Join-Path $RepoRoot ".planning\spikes\turingdb.Dockerfile") (Join-Path $RepoRoot ".planning\spikes")
}

$LogPath = Join-Path $ScriptDir "run.log"
docker run --rm `
  -e "TURINGDB_CONTAINER_IMAGE=$Image" `
  --mount "type=bind,source=$RepoRoot,target=/work" `
  --mount "type=bind,source=$ScratchRoot,target=/scratch" `
  --entrypoint python `
  $Image `
  /work/.planning/spikes/092-turingdb-vector-graphrag-parity/probe_vector.py `
  --scratch /scratch/092-vector-graphrag `
  --out /work/.planning/spikes/092-turingdb-vector-graphrag-parity/results.json 2>&1 | Tee-Object -FilePath $LogPath
exit $LASTEXITCODE
