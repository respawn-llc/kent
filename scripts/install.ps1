param([string]$Version = $env:KENT_VERSION, [string]$InstallDir = "",
    [switch]$Yes, [switch]$NoPath, [switch]$NoDeps, [switch]$Uninstall,
    [switch]$Force, [switch]$NoServiceRestart, [switch]$Help)
Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"
$DefaultRepo = "respawn-llc/kent"
$Repo = $env:KENT_REPO
if ([string]::IsNullOrWhiteSpace($Repo)) {
    $Repo = $DefaultRepo
}
$ReleaseBase = $env:KENT_RELEASE_BASE
if ([string]::IsNullOrWhiteSpace($ReleaseBase)) {
    $ReleaseBase = "https://github.com/$Repo/releases/download"
}
$InstallMarkerName = "kent-install.json"; $UninstallScriptName = "uninstall.ps1"
$UninstallRegistryPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Kent"; $UninstallRegistryDisplayName = "Kent"
function Write-Usage {
    Write-Output @"
Usage: install.ps1 [options]
Installs Kent for the current Windows user.
Options:
  -Version <vX.Y.Z|X.Y.Z>  Release tag to install. Defaults to latest.
  -InstallDir <path>       Install directory. Defaults to ~/.kent/bin.
  -Yes                     Accept prompts.
  -NoPath                  Do not add install directory to User PATH.
  -NoDeps                  Do not offer to install git/rg through winget.
  -Uninstall               Remove Kent binary and installer-owned metadata.
  -Force                   Overwrite an existing kent.exe or symlink.
  -NoServiceRestart        Do not restart an already-installed Kent service.
  -Help                    Show this help.
Environment:
  KENT_VERSION       Override version.
  KENT_REPO          Override repo. Default: respawn-llc/kent.
  KENT_RELEASE_BASE  Override release base URL.
  GITHUB_TOKEN          GitHub token for API rate limits.
  GH_TOKEN              GitHub token for API rate limits.
"@
}
function Fail([string]$Message) {
    Write-Error $Message
    exit 1
}
function Warn([string]$Message) {
    Write-Warning $Message
}
function Get-UserHome {
    if (-not [string]::IsNullOrWhiteSpace($HOME)) {
        return $HOME
    }
    $profileHome = [Environment]::GetFolderPath("UserProfile")
    if (-not [string]::IsNullOrWhiteSpace($profileHome)) {
        return $profileHome
    }
    Fail "Unable to resolve user home directory."
}
function Resolve-InstallDir([string]$Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) {
        $userHome = Get-UserHome
        return [System.IO.Path]::GetFullPath((Join-Path (Join-Path $userHome ".kent") "bin"))
    }
    $expanded = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Value)
    return [System.IO.Path]::GetFullPath($expanded)
}
function Confirm-Action([string]$Question, [bool]$DefaultYes) {
    if ($Yes) {
        return $true
    }
    if (-not [Environment]::UserInteractive) {
        return $false
    }
    $suffix = " [y/N]"
    if ($DefaultYes) {
        $suffix = " [Y/n]"
    }
    $answer = Read-Host ($Question + $suffix)
    if ([string]::IsNullOrWhiteSpace($answer)) {
        return $DefaultYes
    }
    $normalized = $answer.Trim().ToLowerInvariant()
    return $normalized -eq "y" -or $normalized -eq "yes"
}
function Normalize-Version([string]$Value) {
    $trimmed = $Value.Trim()
    if ($trimmed.StartsWith("v")) {
        return $trimmed.Substring(1)
    }
    return $trimmed
}
function Get-AuthHeaders {
    $token = $env:GITHUB_TOKEN
    if ([string]::IsNullOrWhiteSpace($token)) {
        $token = $env:GH_TOKEN
    }
    if ([string]::IsNullOrWhiteSpace($token)) {
        return @{}
    }
    return @{ Authorization = "Bearer $token" }
}
function Resolve-LatestVersion {
    $apiUrl = "https://api.github.com/repos/$Repo/releases/latest"
    $apiMessage = ""
    $rateLimited = $false
    try {
        $release = Invoke-RestMethod -Uri $apiUrl -Headers (Get-AuthHeaders) -UseBasicParsing
        if ($release -ne $null -and -not [string]::IsNullOrWhiteSpace($release.tag_name)) {
            return $release.tag_name
        }
    } catch {
        $apiMessage = $_.Exception.Message
        if ($apiMessage -like "*rate limit*") {
            $rateLimited = $true
        }
    }
    try {
        $latestUrl = "https://github.com/$Repo/releases/latest"
        $response = Invoke-WebRequest -Uri $latestUrl -UseBasicParsing
        $finalUrl = ""
        try {
            if ($null -ne $response.BaseResponse.RequestMessage -and $null -ne $response.BaseResponse.RequestMessage.RequestUri) {
                $finalUrl = $response.BaseResponse.RequestMessage.RequestUri.AbsoluteUri
            }
        } catch {}
        if ([string]::IsNullOrWhiteSpace($finalUrl)) {
            try {
                if ($null -ne $response.BaseResponse.ResponseUri) {
                    $finalUrl = $response.BaseResponse.ResponseUri.AbsoluteUri
                }
            } catch {}
        }
        $marker = "/releases/tag/"
        $index = $finalUrl.IndexOf($marker, [System.StringComparison]::OrdinalIgnoreCase)
        if ($index -ge 0) {
            return $finalUrl.Substring($index + $marker.Length)
        }
    } catch {
        if ([string]::IsNullOrWhiteSpace($apiMessage)) {
            $apiMessage = $_.Exception.Message
        }
    }
    if ($rateLimited) {
        Fail "GitHub API rate limit exceeded. Set GITHUB_TOKEN or GH_TOKEN and retry."
    }
    if (-not [string]::IsNullOrWhiteSpace($apiMessage)) {
        Write-Error $apiMessage
    }
    Fail "Failed to resolve latest version."
}
function Resolve-Arch {
    try {
        $signature = @'
using System;
using System.Runtime.InteropServices;
public static class KentNativeArchitecture {
    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern bool IsWow64Process2(IntPtr process, out ushort processMachine, out ushort nativeMachine);
}
'@
        Add-Type -TypeDefinition $signature -ErrorAction SilentlyContinue | Out-Null
        $processMachine = 0
        $nativeMachine = 0
        $ok = [KentNativeArchitecture]::IsWow64Process2([System.Diagnostics.Process]::GetCurrentProcess().Handle, [ref]$processMachine, [ref]$nativeMachine)
        if ($ok) {
            if ($nativeMachine -eq 0xAA64) { return "arm64" }
            if ($nativeMachine -eq 0x8664) { return "amd64" }
        }
    } catch {}
    $arch = $env:PROCESSOR_ARCHITEW6432
    if ([string]::IsNullOrWhiteSpace($arch)) { $arch = $env:PROCESSOR_ARCHITECTURE }
    if ([string]::IsNullOrWhiteSpace($arch)) { Fail "Unable to resolve Windows architecture." }
    $normalized = $arch.Trim().ToLowerInvariant()
    if ($normalized -eq "amd64" -or $normalized -eq "x64") { return "amd64" }
    if ($normalized -eq "arm64" -or $normalized -eq "aarch64") { return "arm64" }
    Fail "Unsupported Windows architecture: $arch"
}
function New-TempDirectory {
    $parent = [System.IO.Path]::GetTempPath()
    $name = "kent-install-" + [System.Guid]::NewGuid().ToString("N")
    $path = Join-Path $parent $name
    New-Item -ItemType Directory -Path $path | Out-Null
    return $path
}
function Test-HttpResource([string]$Value) {
    return $Value.StartsWith("http://", [System.StringComparison]::OrdinalIgnoreCase) -or $Value.StartsWith("https://", [System.StringComparison]::OrdinalIgnoreCase)
}
function Join-ReleaseResource([string]$Base, [string]$Tag, [string]$Name) {
    if (Test-HttpResource $Base) {
        return $Base.TrimEnd("/") + "/$Tag/$Name"
    }
    return Join-Path (Join-Path $Base $Tag) $Name
}
function Download-File([string]$Url, [string]$Path) {
    if (-not (Test-HttpResource $Url)) {
        if (-not (Test-Path -LiteralPath $Url)) {
            Fail "Release file not found: $Url"
        }
        Copy-Item -LiteralPath $Url -Destination $Path
        return
    }
    try {
        Invoke-WebRequest -Uri $Url -OutFile $Path -UseBasicParsing
    } catch {
        Fail "Failed to download $Url. $($_.Exception.Message)"
    }
}
function Read-Checksum([string]$ChecksumPath, [string]$ArchiveName) {
    $lines = Get-Content -LiteralPath $ChecksumPath
    foreach ($line in $lines) {
        $parts = $line.Split([char[]]" `t", [System.StringSplitOptions]::RemoveEmptyEntries)
        if ($parts.Count -lt 2) {
            continue
        }
        $asset = $parts[1]
        if ($asset.StartsWith("./")) {
            $asset = $asset.Substring(2)
        }
        if ($asset -eq $ArchiveName) {
            return $parts[0].ToLowerInvariant()
        }
    }
    Fail "checksums.txt is missing an entry for $ArchiveName."
}
function Verify-Checksum([string]$ArchivePath, [string]$ChecksumPath, [string]$ArchiveName) {
    $expected = Read-Checksum $ChecksumPath $ArchiveName
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $ArchivePath).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        Fail "Checksum verification failed for $ArchiveName."
    }
}
function Get-UserPathEntries {
    $pathValue = [Environment]::GetEnvironmentVariable("Path", "User")
    if ([string]::IsNullOrWhiteSpace($pathValue)) {
        return @()
    }
    return @($pathValue.Split([char[]]";", [System.StringSplitOptions]::RemoveEmptyEntries))
}
function Normalize-PathDirectory([string]$Directory) {
    $expanded = [Environment]::ExpandEnvironmentVariables($Directory)
    $separators = @([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
    return [System.IO.Path]::GetFullPath($expanded).TrimEnd($separators)
}
function Test-PathEntry([string]$Directory) {
    $full = Normalize-PathDirectory $Directory
    foreach ($entry in Get-UserPathEntries) {
        try {
            $entryFull = Normalize-PathDirectory $entry
        } catch {
            continue
        }
        if ([string]::Equals($entryFull, $full, [System.StringComparison]::OrdinalIgnoreCase)) {
            return $true
        }
    }
    return $false
}
function Add-UserPathEntry([string]$Directory) {
    if (Test-PathEntry $Directory) {
        return $false
    }
    $entries = @(Get-UserPathEntries)
    $entries += $Directory
    [Environment]::SetEnvironmentVariable("Path", ($entries -join ";"), "User")
    return $true
}
function Remove-UserPathEntry([string]$Directory) {
    $full = Normalize-PathDirectory $Directory
    $kept = @()
    $removed = $false
    foreach ($entry in Get-UserPathEntries) {
        try {
            $entryFull = Normalize-PathDirectory $entry
        } catch {
            $kept += $entry
            continue
        }
        if ([string]::Equals($entryFull, $full, [System.StringComparison]::OrdinalIgnoreCase)) {
            $removed = $true
        } else {
            $kept += $entry
        }
    }
    if ($removed) {
        [Environment]::SetEnvironmentVariable("Path", ($kept -join ";"), "User")
    }
    return $removed
}
function New-UninstallScriptContent([string]$Directory) {
    $escapedDirectory = $Directory.Replace("'", "''")
    $template = @'
param(
    [switch]$Yes
)
Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"
$InstallDir = '__INSTALL_DIR__'
$MarkerPath = Join-Path $InstallDir 'kent-install.json'
$RegistryPath = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Kent'
function Confirm-RemovePath([string]$Question) {
    if ($Yes) { return $true }
    if (-not [Environment]::UserInteractive) { return $false }
    $answer = Read-Host ($Question + ' [Y/n]')
    if ([string]::IsNullOrWhiteSpace($answer)) { return $true }
    $normalized = $answer.Trim().ToLowerInvariant()
    return $normalized -eq 'y' -or $normalized -eq 'yes'
}
function Get-UserPathEntries {
    $pathValue = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ([string]::IsNullOrWhiteSpace($pathValue)) { return @() }
    return @($pathValue.Split([char[]]';', [System.StringSplitOptions]::RemoveEmptyEntries))
}
function Normalize-PathDirectory([string]$Directory) {
    $expanded = [Environment]::ExpandEnvironmentVariables($Directory)
    $separators = @([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar)
    return [System.IO.Path]::GetFullPath($expanded).TrimEnd($separators)
}
function Remove-UserPathEntry([string]$Directory) {
    $full = Normalize-PathDirectory $Directory
    $kept = @()
    $removed = $false
    foreach ($entry in Get-UserPathEntries) {
        try {
            $entryFull = Normalize-PathDirectory $entry
        } catch {
            $kept += $entry
            continue
        }
        if ([string]::Equals($entryFull, $full, [System.StringComparison]::OrdinalIgnoreCase)) {
            $removed = $true
        } else {
            $kept += $entry
        }
    }
    if ($removed) {
        [Environment]::SetEnvironmentVariable('Path', ($kept -join ';'), 'User')
    }
    return $removed
}
$pathAdded = $false
if (Test-Path -LiteralPath $MarkerPath) {
    try {
        $marker = Get-Content -LiteralPath $MarkerPath -Raw | ConvertFrom-Json
        $pathAdded = [bool]$marker.PathAdded
    } catch {
        $pathAdded = $false
    }
}
if (Test-Path -LiteralPath (Join-Path $InstallDir 'kent.exe')) {
    Remove-Item -LiteralPath (Join-Path $InstallDir 'kent.exe') -Force
}
if (Test-Path -LiteralPath $MarkerPath) {
    Remove-Item -LiteralPath $MarkerPath -Force
}
if (Test-Path -LiteralPath $RegistryPath) {
    Remove-Item -LiteralPath $RegistryPath -Recurse -Force
}
if ($pathAdded -and (Confirm-RemovePath "Remove $InstallDir from your User PATH?")) {
    [void](Remove-UserPathEntry $InstallDir)
}
$scriptPath = $MyInvocation.MyCommand.Path
Write-Output "Uninstalled Kent. User data under ~/.kent was not removed."
if (Test-Path -LiteralPath $scriptPath) {
    try {
        Remove-Item -LiteralPath $scriptPath -Force
    } catch {
        Write-Warning "Could not remove uninstall script: $($_.Exception.Message)"
    }
}
'@
    return $template.Replace("__INSTALL_DIR__", $escapedDirectory)
}
function Read-InstallerPathOwnership([string]$Directory) {
    $markerPath = Join-Path $Directory $InstallMarkerName
    if (-not (Test-Path -LiteralPath $markerPath)) {
        return $false
    }
    try {
        $marker = Get-Content -LiteralPath $markerPath -Raw | ConvertFrom-Json
        return [bool]$marker.PathAdded
    } catch {
        return $false
    }
}
function Write-InstallMetadata([string]$Directory, [string]$VersionValue, [bool]$PathAdded) {
    $markerPath = Join-Path $Directory $InstallMarkerName
    $uninstallPath = Join-Path $Directory $UninstallScriptName
    $metadata = New-Object PSObject -Property @{
        Version = $VersionValue
        Repo = $Repo
        InstallDir = $Directory
        PathAdded = $PathAdded
        InstalledAtUtc = [DateTime]::UtcNow.ToString("o")
    }
    $metadata | ConvertTo-Json | Set-Content -LiteralPath $markerPath -Encoding UTF8
    New-UninstallScriptContent $Directory | Set-Content -LiteralPath $uninstallPath -Encoding UTF8
}
function Write-UninstallRegistry([string]$Directory, [string]$VersionValue) {
    $uninstallPath = Join-Path $Directory $UninstallScriptName
    if (-not (Test-Path -LiteralPath $UninstallRegistryPath)) { New-Item -Path $UninstallRegistryPath -Force | Out-Null }
    $command = "powershell -NoProfile -ExecutionPolicy Bypass -File `"$uninstallPath`""
    $strings = @{
        DisplayName = $UninstallRegistryDisplayName; DisplayVersion = $VersionValue; Publisher = "Respawn"
        InstallLocation = $Directory; UninstallString = $command; DisplayIcon = (Join-Path $Directory "kent.exe")
    }
    foreach ($name in $strings.Keys) {
        New-ItemProperty -Path $UninstallRegistryPath -Name $name -Value $strings[$name] -PropertyType String -Force | Out-Null
    }
    foreach ($name in @("NoModify", "NoRepair")) {
        New-ItemProperty -Path $UninstallRegistryPath -Name $name -Value 1 -PropertyType DWord -Force | Out-Null
    }
}
function Invoke-Uninstall([string]$Directory) {
    $uninstallPath = Join-Path $Directory $UninstallScriptName
    if (Test-Path -LiteralPath $uninstallPath) {
        $args = @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", $uninstallPath)
        if ($Yes) {
            $args += "-Yes"
        }
        & powershell @args
        exit $LASTEXITCODE
    }
    $target = Join-Path $Directory "kent.exe"
    $marker = Join-Path $Directory $InstallMarkerName
    $pathAdded = $false
    if (Test-Path -LiteralPath $marker) {
        try {
            $parsed = Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json
            $pathAdded = [bool]$parsed.PathAdded
        } catch {
            $pathAdded = $false
        }
    }
    if (Test-Path -LiteralPath $target) {
        Remove-Item -LiteralPath $target -Force
    }
    if (Test-Path -LiteralPath $marker) {
        Remove-Item -LiteralPath $marker -Force
    }
    if (Test-Path -LiteralPath $UninstallRegistryPath) {
        Remove-Item -LiteralPath $UninstallRegistryPath -Recurse -Force
    }
    if ($pathAdded -and (Confirm-Action "Remove $Directory from your User PATH?" $true)) {
        [void](Remove-UserPathEntry $Directory)
    }
    Write-Output "Uninstalled Kent. User data under ~/.kent was not removed."
}
function Install-ArchiveBinary([string]$Source, [string]$Target) {
    if (Test-Path -LiteralPath $Target) {
        $item = Get-Item -LiteralPath $Target -Force
        if ($item.PSIsContainer) {
            Fail "Refusing to overwrite directory $Target"
        }
        $isReparsePoint = (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0)
        if ($isReparsePoint -and -not $Force) {
            Fail "Refusing to overwrite symlink $Target. Re-run with -Force to replace it."
        }
        if (-not $Force -and -not (Confirm-Action "Overwrite existing ${Target}?" $true)) {
            Fail "Install cancelled."
        }
        Remove-Item -LiteralPath $Target -Force
    }
    Copy-Item -LiteralPath $Source -Destination $Target
}
function Test-CommandExists([string]$Name) {
    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    return $cmd -ne $null
}
function Install-MissingDependencies {
    if ($NoDeps) {
        return
    }
    $missing = @()
    if (-not (Test-CommandExists "git")) {
        $missing += New-Object PSObject -Property @{ Name = "git"; Package = "Git.Git" }
    }
    if (-not (Test-CommandExists "rg")) {
        $missing += New-Object PSObject -Property @{ Name = "rg"; Package = "BurntSushi.ripgrep.MSVC" }
    }
    if ($missing.Count -eq 0) {
        return
    }
    $names = @($missing | ForEach-Object { $_.Name }) -join ", "
    if (-not (Test-CommandExists "winget")) {
        Warn "Missing dependencies: $names. Install git and ripgrep manually; Kent can run, but agent search/repo workflows will be degraded."
        return
    }
    if (-not (Confirm-Action "Kent works best with $names. Install missing dependencies with winget now?" $true)) {
        Warn "Kent can run, but agent search/repo workflows will be degraded until git and ripgrep are installed."
        return
    }
    foreach ($dependency in $missing) {
        Write-Output "Installing $($dependency.Name) through winget..."
        & winget install --id $dependency.Package --exact --source winget --accept-source-agreements --accept-package-agreements
        if ($LASTEXITCODE -ne 0) {
            Warn "winget failed to install $($dependency.Name). Kent can run, but agent search/repo workflows may be degraded."
        }
    }
}
function Get-ServiceStatusForUpdate([string]$KentPath) {
    if (-not (Test-Path -LiteralPath $KentPath)) { return $null }
    try {
        $statusResult = & $KentPath service status --json 2>&1
        if ($LASTEXITCODE -ne 0) {
            Warn "Could not inspect Kent background service before update: $statusResult"
            return $null
        }
        return (($statusResult | Out-String) | ConvertFrom-Json)
    } catch {
        Warn "Could not inspect Kent background service before update: $($_.Exception.Message)"
        return $null
    }
}
function Test-ServiceCommandUsesKentPath([object]$Status, [string]$KentPath) {
    if ($Status -eq $null -or $Status.command -eq $null) { return $false }
    $command = @($Status.command)
    if ($command.Count -eq 0) { return $false }
    try {
        $registered = Normalize-PathDirectory ([string]$command[0])
        $target = Normalize-PathDirectory $KentPath
        return [string]::Equals($registered, $target, [System.StringComparison]::OrdinalIgnoreCase)
    } catch { return $false }
}
function Stop-ServiceForUpdate([string]$KentPath) {
    $status = Get-ServiceStatusForUpdate $KentPath
    if ($null -eq $status -or -not [bool]$status.installed) { return $false }
    if (-not (Test-ServiceCommandUsesKentPath $status $KentPath)) { return $false }
    if (-not [bool]$status.running) { return $false }
    if ($NoServiceRestart) {
        Fail "Kent background service is running from $KentPath. Stop it before updating, or rerun without -NoServiceRestart."
    }
    Write-Host "Stopping Kent background service before update..."
    $stopOutput = & $KentPath service stop 2>&1
    if ($LASTEXITCODE -ne 0) { Fail "Failed to stop Kent background service before update: $stopOutput" }
    return $true
}
function Restart-ServiceAfterFailedUpdate([string]$KentPath) {
    if (-not (Test-Path -LiteralPath $KentPath)) {
        Warn "Kent background service was stopped before update, but $KentPath is missing; service may be left stopped."
        return
    }
    try {
        $output = & $KentPath service restart --if-installed 2>&1
        if ($LASTEXITCODE -ne 0) { Warn "Kent background service was stopped before update and restart failed: $output" }
    } catch { Warn "Kent background service was stopped before update and restart failed: $($_.Exception.Message)" }
}
function Restart-ServiceIfInstalled([string]$KentPath) {
    if ($NoServiceRestart) { return }
    try {
        $output = & $KentPath service restart --if-installed 2>&1
        $exitCode = $LASTEXITCODE
        if ($exitCode -ne 0) {
            Warn "Kent background service restart failed after update: $output"
            return
        }
        if (-not [string]::IsNullOrWhiteSpace(($output | Out-String))) {
            Write-Output $output
        }
    } catch {
        Warn "Kent background service restart failed after update: $($_.Exception.Message)"
    }
}
if ($Help) {
    Write-Usage
    exit 0
}
$resolvedInstallDir = Resolve-InstallDir $InstallDir
if ($Uninstall) {
    Invoke-Uninstall $resolvedInstallDir
    exit 0
}
$tag = $Version
if ([string]::IsNullOrWhiteSpace($tag)) {
    $tag = Resolve-LatestVersion
}
if ([string]::IsNullOrWhiteSpace($tag)) {
    Fail "Failed to resolve version."
}
if (-not $tag.StartsWith("v")) {
    $tag = "v$tag"
}
$normalizedVersion = Normalize-Version $tag
$arch = Resolve-Arch
$assetBase = "kent_${normalizedVersion}_windows_${arch}"
$archiveName = "$assetBase.zip"
$binaryName = "$assetBase.exe"
$archiveUrl = Join-ReleaseResource $ReleaseBase $tag $archiveName
$checksumsUrl = Join-ReleaseResource $ReleaseBase $tag "checksums.txt"
$tempDir = New-TempDirectory
$target = ""
$serviceStoppedForUpdate = $false
$installSucceeded = $false
try {
    $archivePath = Join-Path $tempDir $archiveName
    $checksumPath = Join-Path $tempDir "checksums.txt"
    Download-File $archiveUrl $archivePath
    Download-File $checksumsUrl $checksumPath
    Verify-Checksum $archivePath $checksumPath $archiveName
    Expand-Archive -LiteralPath $archivePath -DestinationPath $tempDir -Force
    $extractedBinary = Join-Path $tempDir $binaryName
    if (-not (Test-Path -LiteralPath $extractedBinary)) {
        Fail "Archive $archiveName did not contain $binaryName."
    }
    New-Item -ItemType Directory -Path $resolvedInstallDir -Force | Out-Null
    $target = Join-Path $resolvedInstallDir "kent.exe"
    $serviceStoppedForUpdate = [bool](Stop-ServiceForUpdate $target)
    Install-ArchiveBinary $extractedBinary $target
    $versionResult = & $target --version 2>&1
    $versionExitCode = $LASTEXITCODE
    $versionOutput = ($versionResult | Out-String).Trim()
    if ($versionExitCode -ne 0 -or $versionOutput -ne $normalizedVersion) {
        Fail "Installed binary version check failed: got '$versionOutput', want '$normalizedVersion'."
    }
    $pathAdded = Read-InstallerPathOwnership $resolvedInstallDir
    if (-not $NoPath) {
        if (Test-PathEntry $resolvedInstallDir) {
            Write-Output "$resolvedInstallDir is already on User PATH."
        } elseif (Confirm-Action "Add $resolvedInstallDir to your User PATH so 'kent' works in new terminals?" $true) {
            $pathAdded = Add-UserPathEntry $resolvedInstallDir
            Write-Output "Added $resolvedInstallDir to User PATH. Restart your terminal to use kent from PATH."
        } else {
            Write-Output "Skipped PATH update. Run kent with $target or add $resolvedInstallDir to your User PATH."
        }
    }
    Write-InstallMetadata $resolvedInstallDir $normalizedVersion $pathAdded
    try {
        Write-UninstallRegistry $resolvedInstallDir $normalizedVersion
    } catch {
        Warn "Could not register Kent in Windows Apps & Features: $($_.Exception.Message)"
    }
    Install-MissingDependencies
    Restart-ServiceIfInstalled $target
    $installSucceeded = $true
    Write-Output "Installed kent $normalizedVersion to $target"

    # Upgrade safety net for users coming from the old Builder release. Kent never
    # reads ~/.builder and this installer never runs the migration for you (it
    # mutates your DB and filesystem). Surface the steps so the user runs them
    # explicitly. A reparse point (the post-migration ~/.builder -> ~/.kent compat
    # link) is benign and intentionally ignored here.
    $oldBuilderRoot = Join-Path (Get-UserHome) ".builder"
    if (Test-Path -LiteralPath $oldBuilderRoot) {
        $oldItem = Get-Item -LiteralPath $oldBuilderRoot -Force
        $isReparse = (($oldItem.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0)
        if ($oldItem.PSIsContainer -and -not $isReparse) {
            $compatUrl = "$ReleaseBase/builder-2.0.0/builder_2.0.0_windows_$arch.exe"
            Write-Output ""
            Write-Output "Found an existing ~/.builder from the previous Builder release."
            Write-Output "Kent uses ~/.kent and does not read ~/.builder. To migrate your sessions,"
            Write-Output "worktrees, and config, run the one-time Builder 2.0 migration BEFORE using kent:"
            Write-Output "  1. Stop any running Builder activity (interactive sessions and the service)."
            Write-Output "  2. Download the Builder 2.0 migration binary and run the migration:"
            Write-Output ("       Invoke-WebRequest -Uri '" + $compatUrl + "' -OutFile '.\builder-migrate.exe'")
            Write-Output "       .\builder-migrate.exe migrate"
            Write-Output "  3. Re-run this installer if needed."
        }
    }
} finally {
    if ($serviceStoppedForUpdate -and -not $installSucceeded) {
        Restart-ServiceAfterFailedUpdate $target
    }
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
