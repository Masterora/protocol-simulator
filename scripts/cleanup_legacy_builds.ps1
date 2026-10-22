$ErrorActionPreference='Stop'
$workspace=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$build=[IO.Path]::GetFullPath((Join-Path $workspace 'build/bin'))
$legacy=[IO.Path]::GetFullPath((Join-Path $workspace 'outputs/gytest-go'))
$retired=[IO.Path]::GetFullPath((Join-Path $workspace 'work/superseded-protocol-simulator'))
if(-not $build.StartsWith($workspace+'\build\') -or -not $legacy.StartsWith($workspace+'\outputs\') -or -not $retired.StartsWith($workspace+'\work\')) {throw 'Invalid cleanup roots'}
$names=@('GYLab-multidevice.exe','GYLab-replica.exe','GYLab-native.exe','GYLab-controls.exe','GYLab-automatic.exe')
for($attempt=0;$attempt -lt 2880;$attempt++) {
    $pending=$false
    foreach($name in $names) {
        $file=Join-Path $build $name
        if(Test-Path -LiteralPath $file) {
            try {Remove-Item -LiteralPath $file -Force -ErrorAction Stop} catch {$pending=$true}
        }
    }
    if(Test-Path -LiteralPath $retired) {
        foreach($file in (Get-ChildItem -LiteralPath $retired -Filter 'protocol-simulator-*.exe' -File)) {
            try {Remove-Item -LiteralPath $file.FullName -Force -ErrorAction Stop} catch {$pending=$true}
        }
        if(@(Get-ChildItem -LiteralPath $retired -Force).Count -eq 0) {
            try {Remove-Item -LiteralPath $retired -Force -ErrorAction Stop} catch {$pending=$true}
        }
    }
    # The source has moved. Remove only empty old directories, never user data.
    foreach($dir in @((Join-Path $legacy 'build/bin'),(Join-Path $legacy 'build'),$legacy)) {
        if(Test-Path -LiteralPath $dir) {
            if(@(Get-ChildItem -LiteralPath $dir -Force).Count -eq 0) {
                try {Remove-Item -LiteralPath $dir -Force -ErrorAction Stop} catch {$pending=$true}
            }
        }
    }
    if(-not $pending){break}
    Start-Sleep -Seconds 5
}
