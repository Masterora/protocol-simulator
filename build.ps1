param(
    [string]$Proxy = '',
    [string]$CacheRoot = (Join-Path $PSScriptRoot 'work/golang')
)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    New-Item -ItemType Directory -Path work -Force | Out-Null
    if (-not (Test-Path -LiteralPath 'work/go.mod')) { Set-Content -LiteralPath 'work/go.mod' -Value "module protocol-simulator/work`n`ngo 1.23" -Encoding utf8 }
    $env:GOENV = 'off'
    $env:GOTELEMETRYDIR = Join-Path $CacheRoot 'telemetry'
    $env:GOCACHE = Join-Path $CacheRoot 'cache'
    $env:GOMODCACHE = Join-Path $CacheRoot 'mod'
    $env:GOPATH = Join-Path $CacheRoot 'path'
    if ($Proxy) { $env:GOPROXY = $Proxy }
    go telemetry off
    if ($LASTEXITCODE) { throw 'Cannot configure local Go telemetry directory' }
    go mod download
    if ($LASTEXITCODE) { throw 'Dependency download failed' }
    New-Item -ItemType Directory -Path verification -Force | Out-Null
    go test ./... -count=1 -v -timeout=60s > verification/brand-go-tests.txt
    if ($LASTEXITCODE) { throw 'Tests failed' }
    go vet ./...
    if ($LASTEXITCODE) { throw 'go vet failed' }
    go build -tags desktop,production -ldflags '-H windowsgui' -o build/bin/protocol-simulator.exe .
    if ($LASTEXITCODE) { throw 'Build failed' }
    & (Join-Path $PSScriptRoot 'build/windows/resources.ps1') -Executable (Join-Path $PSScriptRoot 'build/bin/protocol-simulator.exe')
    Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot 'build/bin') -Filter '*.exe' -File | Where-Object Name -ne 'protocol-simulator.exe' | Remove-Item
    node --test tests/ui.test.mjs > verification/brand-ui-tests.txt
    if ($LASTEXITCODE) { throw 'UI tests failed; run npm ci first if dependencies are missing' }
    $previousSmokeBinary = $env:GYLAB_NATIVE_SMOKE_BINARY
    try {
        $env:GYLAB_NATIVE_SMOKE_BINARY = Join-Path $PSScriptRoot 'build/bin/protocol-simulator.exe'
        go test . -run '^TestNativeWindowSmoke$' -count=1 -v -timeout=180s > verification/brand-window-smoke.txt
        if ($LASTEXITCODE) { throw 'Native window verification failed' }
    } finally { $env:GYLAB_NATIVE_SMOKE_BINARY = $previousSmokeBinary }
    Write-Output (Join-Path $PSScriptRoot 'build\bin\protocol-simulator.exe')
} finally {
    Pop-Location
}
