param(
  [Parameter(Mandatory=$true)]
  [string]$Path
)

$resolved = Resolve-Path -LiteralPath $Path
$bytes = [System.IO.File]::ReadAllBytes($resolved)
if ($bytes.Length -lt 0x100) {
  throw "file too small to be a PE executable: $resolved"
}

$peOffset = [BitConverter]::ToInt32($bytes, 0x3C)
if ($peOffset -lt 0 -or $peOffset + 0x5C -ge $bytes.Length) {
  throw "invalid PE header offset in $resolved"
}

$signature = [System.Text.Encoding]::ASCII.GetString($bytes, $peOffset, 4)
if ($signature -ne "PE`0`0") {
  throw "missing PE signature in $resolved"
}

$optionalHeaderOffset = $peOffset + 24
$subsystemOffset = $optionalHeaderOffset + 68
$subsystem = [BitConverter]::ToUInt16($bytes, $subsystemOffset)

switch ($subsystem) {
  2 { "windows gui subsystem ok: $resolved"; exit 0 }
  3 { throw "windows console subsystem found in $resolved" }
  default { throw "unexpected PE subsystem $subsystem in $resolved" }
}
