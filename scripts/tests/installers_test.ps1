Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"
$repoRoot = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$installer = Join-Path $repoRoot "scripts/install.ps1"
$registryPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\Kent"
if (Test-Path -LiteralPath $registryPath) {
    throw "Run installer smoke coverage on an isolated Windows CI user; Kent uninstall metadata already exists."
}
$root = Join-Path ([IO.Path]::GetTempPath()) ("kent-installer-test-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $root | Out-Null
try {
    $fixture = Join-Path $root "fixture.exe"
    Add-Type -OutputAssembly $fixture -OutputType ConsoleApplication -TypeDefinition @'
using System;
using System.IO;
public static class InstallerFixture {
    public static int Main(string[] args) {
        File.AppendAllText(Environment.GetEnvironmentVariable("FIXTURE_CALLS"),
            String.Join(" ", args) + Environment.NewLine);
        if (args.Length == 1 && args[0] == "--version") {
            Console.WriteLine("9.9.9");
            return 0;
        }
        if (args.Length < 2 || args[0] != "service") return 91;
        switch (args[1]) {
            case "status":
                bool installed = Environment.GetEnvironmentVariable("FIXTURE_INSTALLED") == "true";
                bool running = Environment.GetEnvironmentVariable("FIXTURE_RUNNING") == "true";
                string path = Environment.GetEnvironmentVariable("FIXTURE_TARGET")
                    .Replace("\\", "\\\\").Replace("\"", "\\\"");
                Console.WriteLine("{\"installed\":" + installed.ToString().ToLowerInvariant() +
                    ",\"running\":" + running.ToString().ToLowerInvariant() +
                    ",\"command\":[\"" + path + "\"]}");
                return 0;
            case "install":
                Console.Error.WriteLine("fixture service child diagnostics");
                return Int32.Parse(Environment.GetEnvironmentVariable("FIXTURE_INSTALL_EXIT"));
            case "stop":
            case "restart":
                return 0;
            default:
                return 91;
        }
    }
}
'@
    $releaseBase = Join-Path $root "releases"
    $release = Join-Path $releaseBase "v9.9.9"
    New-Item -ItemType Directory -Path $release -Force | Out-Null
    $checksums = @()
    foreach ($arch in @("amd64", "arm64")) {
        $name = "kent_9.9.9_windows_$arch"
        $binary = Join-Path $root "$name.exe"
        Copy-Item -LiteralPath $fixture -Destination $binary
        $archive = Join-Path $release "$name.zip"
        Compress-Archive -LiteralPath $binary -DestinationPath $archive
        $checksums += (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash + "  ./$name.zip"
    }
    $checksums | Set-Content -LiteralPath (Join-Path $release "checksums.txt")
    $env:KENT_RELEASE_BASE = $releaseBase
    $env:FIXTURE_INSTALL_EXIT = "0"
    $env:FIXTURE_INSTALLED = "false"
    $env:FIXTURE_RUNNING = "false"
    $emptyInput = Join-Path $root "stdin"
    New-Item -ItemType File -Path $emptyInput | Out-Null

    function Invoke-Case([string]$Name, [string[]]$Options, [bool]$Success, [string[]]$ExpectedCalls,
        [string]$Directory = (Join-Path $root $Name)) {
        $env:FIXTURE_CALLS = Join-Path $root "$Name.calls"
        $env:FIXTURE_TARGET = Join-Path $Directory "kent.exe"
        $stdout = Join-Path $root "$Name.stdout"
        $stderr = Join-Path $root "$Name.stderr"
        $arguments = @("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "`"$installer`"",
            "-Version", "9.9.9", "-InstallDir", "`"$Directory`"", "-NoPath", "-NoDeps") + $Options
        $process = Start-Process powershell -ArgumentList $arguments -Wait -PassThru -NoNewWindow `
            -RedirectStandardInput $emptyInput -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        if (($process.ExitCode -eq 0) -ne $Success) {
            Get-Content -LiteralPath $stdout, $stderr | Write-Output
            throw "$Name returned unexpected exit code $($process.ExitCode)"
        }
        $actual = @()
        if (Test-Path -LiteralPath $env:FIXTURE_CALLS) {
            $actual = @(Get-Content -LiteralPath $env:FIXTURE_CALLS)
        }
        if ($actual.Count -ne $ExpectedCalls.Count) {
            throw "$Name commands: $($actual -join ', '); expected: $($ExpectedCalls -join ', ')"
        }
        for ($index = 0; $index -lt $actual.Count; $index++) {
            if (-not [string]::Equals($actual[$index], $ExpectedCalls[$index], [StringComparison]::Ordinal)) {
                throw "$Name command ${index}: '$($actual[$index])'; expected: '$($ExpectedCalls[$index])'"
            }
        }
        Write-Output "PASS $Name"
    }
    $acceptedCalls = @("--version", "service install")
    $skippedCalls = @("--version", "service restart --if-installed")
    Invoke-Case "explicit" @("-Service") $true $acceptedCalls
    Invoke-Case "yes" @("-Yes") $true $acceptedCalls
    Invoke-Case "skip" @("-Yes", "-NoService") $true $skippedCalls
    Invoke-Case "unattended" @() $true $skippedCalls
    Invoke-Case "fresh-no-restart" @("-Yes", "-NoServiceRestart") $true @("--version")
    Invoke-Case "conflicting-service-restart" @("-Service", "-NoServiceRestart") $false @()
    if (Test-Path -LiteralPath (Join-Path $root "conflicting-service-restart/kent.exe")) {
        throw "Conflicting service options installed a binary"
    }
    $env:FIXTURE_INSTALL_EXIT = "37"
    Invoke-Case "failure" @("-Service") $false $acceptedCalls
    if (-not (Test-Path -LiteralPath (Join-Path $root "failure/kent.exe"))) {
        throw "Service failure removed the installed binary"
    }
    $diagnostics = Get-Content -LiteralPath (Join-Path $root "failure.stderr") -Raw
    if (-not $diagnostics.Contains("fixture service child diagnostics")) {
        throw "Service failure lost child diagnostics"
    }
    $env:FIXTURE_INSTALL_EXIT = "0"
    $env:FIXTURE_INSTALLED = "true"
    $env:FIXTURE_RUNNING = "true"
    $existing = Join-Path $root "explicit"
    Invoke-Case "upgrade" @("-Yes", "-NoService") $true `
        @("service status --json", "service stop", "--version", "service restart --if-installed") $existing
    Invoke-Case "no-restart" @("-Yes", "-NoService", "-NoServiceRestart") $false `
        @("service status --json") $existing
    Invoke-Case "upgrade-offer" @("-Yes") $true `
        @("service status --json", "service stop", "--version", "service install") $existing
    $env:FIXTURE_RUNNING = "false"
    Invoke-Case "stopped-no-restart" @("-Yes", "-NoServiceRestart") $true `
        @("service status --json", "--version") $existing
    $env:FIXTURE_RUNNING = "true"
    $env:FIXTURE_INSTALL_EXIT = "12"
    Invoke-Case "upgrade-offer-failure" @("-Yes") $false `
        @("service status --json", "service stop", "--version", "service install") $existing
} finally {
    # This key did not exist before the fixture ran. No real Kent service was used.
    if (Test-Path -LiteralPath $registryPath) {
        Remove-Item -LiteralPath $registryPath -Recurse -Force
    }
    Remove-Item -LiteralPath $root -Recurse -Force
}
