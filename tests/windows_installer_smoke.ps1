param(
    [Parameter(Mandatory=$true)][string]$Setup,
    [Parameter(Mandatory=$true)][string]$Application
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$Setup = (Resolve-Path -LiteralPath $Setup).Path
$Application = (Resolve-Path -LiteralPath $Application).Path
$registryPath = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\protocol-simulator'
$registry = [Microsoft.Win32.RegistryKey]::OpenBaseKey('CurrentUser', 'Registry32')
$desktopShortcut = Join-Path ([Environment]::GetFolderPath('Desktop')) 'protocol-simulator.lnk'
$menu = Join-Path ([Environment]::GetFolderPath('Programs')) 'protocol-simulator'
if ($registry.OpenSubKey($registryPath) -or (Test-Path $desktopShortcut) -or (Test-Path $menu)) {
    throw 'Use a clean Windows test profile: an existing protocol-simulator installation must not be changed.'
}
$install = Join-Path ([IO.Path]::GetTempPath()) ('protocol-simulator install-' + [Guid]::NewGuid().ToString('N'))
$appProcess = $null
$expectedHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Application).Hash

function Require([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}
function Run-Setup {
    $p = Start-Process -FilePath $Setup -ArgumentList @('/S', "/D=$install") -Wait -PassThru
    return $p.ExitCode
}
function Run-Uninstall {
    $p = Start-Process -FilePath (Join-Path $install 'Uninstall.exe') -ArgumentList @('/S', "_?=$install") -Wait -PassThru
    return $p.ExitCode
}

try {
    Require ((Run-Setup) -eq 0) 'Initial installation failed (WebView2 installation may require network).'
    $installed = Join-Path $install 'protocol-simulator.exe'
    Require ((Get-FileHash -Algorithm SHA256 -LiteralPath $installed).Hash -eq $expectedHash) 'Installed application differs from verified payload.'
    $entry = $registry.OpenSubKey($registryPath)
    Require ($null -ne $entry) 'Uninstall registration missing.'
    Require ($entry.GetValue('InstallLocation') -eq $install) 'Wrong registered install path.'
    $entry.Close()
    $shell = New-Object -ComObject WScript.Shell
    Require ($shell.CreateShortcut($desktopShortcut).TargetPath -eq $installed) 'Desktop shortcut target is wrong.'
    $menuShortcut = Join-Path $menu 'protocol-simulator.lnk'
    Require ($shell.CreateShortcut($menuShortcut).TargetPath -eq $installed) 'Start Menu shortcut target is wrong.'
    $project = Join-Path $install 'user-project.json'
    $userData = '{"name":"keep my project"}'
    [IO.File]::WriteAllText($project, $userData)
    $example = Get-ChildItem -LiteralPath (Join-Path $install 'examples') -File | Select-Object -First 1
    [IO.File]::WriteAllText($example.FullName, $userData)

    $config = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('{"name":"Installer acceptance","role":"client"}')).TrimEnd('=').Replace('+','-').Replace('/','_')
    $appProcess = Start-Process -FilePath $installed -ArgumentList "--protocol-project=$config" -PassThru
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        Start-Sleep -Milliseconds 200
        $appProcess.Refresh()
        if ($appProcess.HasExited) { throw 'Installed app exited before its main window became ready.' }
    } until ($appProcess.MainWindowTitle -like '*Installer acceptance*' -or [DateTime]::UtcNow -ge $deadline)
    Require ($appProcess.MainWindowTitle -like '*Installer acceptance*') 'Installed WebView frontend did not finish startup.'
    Require ((Run-Setup) -eq 2) 'Upgrade must refuse to modify a running application.'
    Require ((Run-Uninstall) -eq 2) 'Uninstall must refuse to modify a running application.'
    Require (-not $appProcess.HasExited) 'Installer interrupted the running project.'
    [void]$appProcess.CloseMainWindow()
    Require ($appProcess.WaitForExit(10000)) 'Application did not close normally.'
    $appProcess = $null

    # A changed executable must be replaced, while editable project files survive.
    $stream = [IO.File]::Open($installed, 'Append')
    $stream.WriteByte(0)
    $stream.Dispose()
    Require ((Run-Setup) -eq 0) 'Upgrade/reinstall failed.'
    Require ((Get-FileHash -Algorithm SHA256 -LiteralPath $installed).Hash -eq $expectedHash) 'Upgrade did not restore the new executable.'
    Require ([IO.File]::ReadAllText($project) -eq $userData) 'Upgrade changed a user project.'
    Require ([IO.File]::ReadAllText($example.FullName) -eq $userData) 'Upgrade changed an editable example.'
    Require ((Run-Uninstall) -eq 0) 'Uninstall failed.'
    Require (-not (Test-Path -LiteralPath $installed)) 'Uninstall retained the program executable.'
    Require (-not (Test-Path -LiteralPath $desktopShortcut)) 'Uninstall retained the desktop shortcut.'
    Require ($null -eq $registry.OpenSubKey($registryPath)) 'Uninstall retained its registration.'
    Require ([IO.File]::ReadAllText($project) -eq $userData) 'Uninstall removed a user project.'
    Require ([IO.File]::ReadAllText($example.FullName) -eq $userData) 'Uninstall removed an editable example.'
    $report = @{
        passed = $true
        setupSHA256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $Setup).Hash
        applicationSHA256 = $expectedHash
        installationVerified = $true
        installedAppStartupVerified = $true
        upgradeVerified = $true
        uninstallVerified = $true
        runningApplicationProtected = $true
        userProjectsAndEditedExamplesPreserved = $true
    }
    $report | ConvertTo-Json | Set-Content -LiteralPath 'verification/windows-installer-runtime.json' -Encoding utf8
} finally {
    if ($appProcess -and -not $appProcess.HasExited) {
        [void]$appProcess.CloseMainWindow()
        if (-not $appProcess.WaitForExit(5000)) { $appProcess.Kill(); $appProcess.WaitForExit() }
    }
    if (Test-Path -LiteralPath (Join-Path $install 'Uninstall.exe')) { [void](Run-Uninstall) }
    # This directory was generated uniquely by this test, after the clean-profile guard.
    if (Test-Path -LiteralPath $install) { Remove-Item -LiteralPath $install -Recurse -Force }
    $registry.Close()
}
