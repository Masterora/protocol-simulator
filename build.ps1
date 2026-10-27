param(
    [string]$Proxy = '',
    [string]$CacheRoot = (Join-Path $PSScriptRoot 'work/golang')
)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    New-Item -ItemType Directory -Path work -Force | Out-Null
    if (-not (Test-Path -LiteralPath 'work/go.mod')) { Set-Content -LiteralPath 'work/go.mod' -Value "module protocol-simulator/work`n`ngo 1.25" -Encoding utf8 }
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
    python scripts/delivery.py begin --platform windows
    if ($LASTEXITCODE) { throw 'Cannot begin build evidence' }
    npm run check > verification/quality-checks.txt 2>&1
    if ($LASTEXITCODE) { throw 'Source quality checks failed; run npm ci first' }
    python -m unittest discover -s tests -p 'test_*.py' -v > verification/packaging-tests.txt 2>&1
    if ($LASTEXITCODE) { throw 'Packaging checks failed' }
    go test ./... -count=1 -v -timeout=60s > verification/brand-go-tests.txt 2>&1
    if ($LASTEXITCODE) { throw 'Tests failed' }
    go vet ./... > verification/vet.txt 2>&1
    if ($LASTEXITCODE) { throw 'go vet failed' }
    go build -tags desktop,production -ldflags '-H windowsgui' -o build/bin/protocol-simulator.exe .
    if ($LASTEXITCODE) { throw 'Build failed' }
    & (Join-Path $PSScriptRoot 'build/windows/resources.ps1') -Executable (Join-Path $PSScriptRoot 'build/bin/protocol-simulator.exe')
    node --test tests/ui.test.mjs > verification/brand-ui-tests.txt 2>&1
    if ($LASTEXITCODE) { throw 'UI tests failed; run npm ci first if dependencies are missing' }
    $previousSmokeBinary = $env:GYLAB_NATIVE_SMOKE_BINARY
    try {
        $env:GYLAB_NATIVE_SMOKE_BINARY = Join-Path $PSScriptRoot 'build/bin/protocol-simulator.exe'
        go test . -run '^TestNativeWindowSmoke$' -count=1 -v -timeout=180s > verification/brand-window-smoke.txt 2>&1
        if ($LASTEXITCODE) { throw 'Native window verification failed' }
    } finally { $env:GYLAB_NATIVE_SMOKE_BINARY = $previousSmokeBinary }
    python scripts/delivery.py record --platform windows
    if ($LASTEXITCODE) { throw 'Build evidence verification failed' }
    Write-Output (Join-Path $PSScriptRoot 'build\bin\protocol-simulator.exe')
} finally {
    Pop-Location
}
