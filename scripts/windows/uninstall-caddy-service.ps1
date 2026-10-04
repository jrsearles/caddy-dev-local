#requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string] $ServiceName = 'DevLocalCaddy',
    [string] $DataDirectory = 'C:\ProgramData\DevLocal',
    [string] $NssmExe = 'C:\ProgramData\chocolatey\bin\nssm.exe',
    [switch] $RemoveConfig,
    [switch] $RemoveLogs,
    [switch] $RemoveStorage,
    [switch] $RemoveDataDirectory
)

$ErrorActionPreference = 'Stop'

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Resolve-Nssm {
    param([string] $Path)

    if ($Path) {
        if (-not (Test-Path -LiteralPath $Path)) { throw "nssm.exe not found at $Path" }
        return (Resolve-Path -LiteralPath $Path).Path
    }
    foreach ($candidate in @(
        'C:\ProgramData\chocolatey\bin\nssm.exe',
        'C:\ProgramData\chocolatey\lib\NSSM\tools\nssm.exe')) {
        if (Test-Path -LiteralPath $candidate) { return (Resolve-Path -LiteralPath $candidate).Path }
    }
    $command = Get-Command nssm -ErrorAction SilentlyContinue
    if ($command) { return $command.Source }
    throw 'nssm.exe not found. Looked in the Chocolatey directories and on PATH.'
}

if (-not (Test-Administrator)) {
    throw 'This script must be run from an Administrator PowerShell session.'
}

$nssm = Resolve-Nssm -Path $NssmExe

$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $service) {
    Write-Host "Service $ServiceName is not installed. Nothing to remove."
}
else {
    if ($service.Status -ne 'Stopped') {
        Write-Host "Stopping $ServiceName"
        Stop-Service -Name $ServiceName -Force
        $service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
    }
    Write-Host "Removing $ServiceName"
    & $nssm remove $ServiceName confirm | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "nssm remove failed with exit code $LASTEXITCODE" }
    Write-Host "Removed $ServiceName"
}

$configPath = Join-Path $DataDirectory 'Caddyfile'
$stdoutLog = Join-Path $DataDirectory 'caddy.log'
$stderrLog = Join-Path $DataDirectory 'caddy.err.log'
$storageDirectory = Join-Path $DataDirectory 'caddy-data'

if ($RemoveLogs) {
    foreach ($log in @($stdoutLog, $stderrLog)) {
        if (Test-Path -LiteralPath $log) {
            Remove-Item -LiteralPath $log -Force
            Write-Host "Removed $log"
        }
    }
}

if ($RemoveConfig) {
    if (Test-Path -LiteralPath $configPath) {
        Remove-Item -LiteralPath $configPath -Force
        Write-Host "Removed $configPath"
    }
}

if ($RemoveStorage) {
    if (Test-Path -LiteralPath $storageDirectory) {
        Write-Warning "$storageDirectory holds the Caddy root certificate and issued certificates."
        Write-Warning 'Deleting it invalidates every certificate that CA has issued and any trust you granted it.'
        Remove-Item -LiteralPath $storageDirectory -Recurse -Force
        Write-Host "Removed $storageDirectory"
    }
}

if ($RemoveDataDirectory) {
    if (Test-Path -LiteralPath $DataDirectory) {
        Write-Warning "Removing the whole $DataDirectory, including devlocal settings, status and logs."
        Remove-Item -LiteralPath $DataDirectory -Recurse -Force
        Write-Host "Removed $DataDirectory"
    }
}

Write-Host ''
Write-Host 'Deliberately left alone:'
Write-Host '  caddy.exe and the Chocolatey caddy package'
Write-Host '  the DevLocal Windows service'
Write-Host "  the hosts file (run 'devlocal clean' first if you want managed entries removed)"
Write-Host "  generated UI files under the devlocal index directory"
if (Test-Path -LiteralPath $configPath) { Write-Host "  $configPath (pass -RemoveConfig to delete)" }
if (Test-Path -LiteralPath $storageDirectory) { Write-Host "  $storageDirectory (pass -RemoveStorage to delete)" }