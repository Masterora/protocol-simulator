param([Parameter(Mandatory=$true)][string]$Executable)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$binary = (Resolve-Path -LiteralPath $Executable).Path
Push-Location (Join-Path $root 'scripts/windows-resources')
try {
    go run . $root $binary
    if ($LASTEXITCODE) { throw 'Windows resource generation failed' }
} finally {
    Pop-Location
}
