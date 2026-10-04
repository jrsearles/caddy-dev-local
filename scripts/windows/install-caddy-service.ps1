#requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string] $ServiceName = 'DevLocalCaddy',
    [string] $DisplayName = 'Caddy (devlocal)',
    [string] $DataDirectory = 'C:\ProgramData\DevLocal',
    [string] $CaddyExe = 'C:\ProgramData\chocolatey\lib\caddy\tools\caddy.exe',
    [string] $NssmExe = 'C:\ProgramData\chocolatey\bin\nssm.exe',
    [int] $AdminPort = 2019,
    [int] $HttpPort = 8080,
    [switch] $KeepRunningCaddy,
    [switch] $ForceConfig
)

$ErrorActionPreference = 'Stop'

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = [Security.Principal.WindowsPrincipal]::new($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Resolve-Executable {
    param([string] $Path, [string[]] $Candidates, [string] $Label)

    if ($Path) {
        if (-not (Test-Path -LiteralPath $Path)) { throw "$Label not found at $Path" }
        return (Resolve-Path -LiteralPath $Path).Path
    }
    foreach ($candidate in $Candidates) {
        if (Test-Path -LiteralPath $candidate) { return (Resolve-Path -LiteralPath $candidate).Path }
    }
    $found = $null
    foreach ($candidate in $Candidates) {
        $command = Get-Command $candidate -ErrorAction SilentlyContinue
        if ($command) { $found = $command.Source; break }
    }
    if (-not $found) { throw "$Label not found. Looked in: $($Candidates -join ', ')" }
    return $found
}

function Stop-ManualCaddy {
    param([int] $Port)

    $listeners = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    if (-not $listeners) { return }
    $pids = $listeners | Select-Object -ExpandProperty OwningProcess -Unique
    foreach ($processId in $pids) {
        $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
        if (-not $process) { continue }
        if ($process.ProcessName -ne 'caddy') {
            throw "Port $Port is held by $($process.ProcessName) (PID $processId). Stop it and re-run."
        }
        Write-Host "Stopping manually running caddy (PID $processId) holding port $Port"
        Stop-Process -Id $processId -Force
    }
    Start-Sleep -Seconds 2
}

function Write-DefaultCaddyfile {
    param([string] $Path, [int] $Admin, [int] $Http, [string] $Storage)

    $content = @"
{
	admin localhost:$Admin
	http_port $Http
	auto_https off
	storage file_system {
		root $Storage
	}
}

http://localhost:$Http {
	respond "caddy-dev-local placeholder"
}
"@
    Set-Content -LiteralPath $Path -Value $content -Encoding utf8NoBOM
    Write-Host "Wrote $Path"
}

function Wait-ForAdminApi {
    param([int] $Port, [int] $TimeoutSeconds = 30)

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-WebRequest -Uri "http://localhost:$Port/config/" -TimeoutSec 3 -UseBasicParsing
            if ($response.StatusCode -eq 200) { return $true }
        }
        catch {
            Start-Sleep -Milliseconds 500
        }
    }
    return $false
}

if (-not (Test-Administrator)) {
    throw 'This script must be run from an Administrator PowerShell session.'
}

$caddy = Resolve-Executable -Path $CaddyExe -Label 'caddy.exe' -Candidates @(
    'C:\ProgramData\chocolatey\lib\caddy\tools\caddy.exe', "$env:ProgramFiles\caddy\caddy.exe", 'caddy')
$nssm = Resolve-Executable -Path $NssmExe -Label 'nssm.exe' -Candidates @(
    'C:\ProgramData\chocolatey\bin\nssm.exe', 'C:\ProgramData\chocolatey\lib\NSSM\tools\nssm.exe', 'nssm')

Write-Host "caddy: $caddy"
Write-Host "nssm:  $nssm"

$configDirectory = $DataDirectory
$configPath = Join-Path $configDirectory 'Caddyfile'
$storageDirectory = Join-Path $configDirectory 'caddy-data'
$stdoutLog = Join-Path $configDirectory 'caddy.log'
$stderrLog = Join-Path $configDirectory 'caddy.err.log'

foreach ($directory in @($configDirectory, $storageDirectory)) {
    if (-not (Test-Path -LiteralPath $directory)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
        Write-Host "Created $directory"
    }
}

if ($ForceConfig -or -not (Test-Path -LiteralPath $configPath)) {
    Write-DefaultCaddyfile -Path $configPath -Admin $AdminPort -Http $HttpPort -Storage $storageDirectory
}
else {
    Write-Host "Keeping existing $configPath (use -ForceConfig to regenerate)"
}

Write-Host 'Validating Caddyfile'
& $caddy fmt --overwrite $configPath | Out-Null
& $caddy validate --config $configPath --adapter caddyfile
if ($LASTEXITCODE -ne 0) { throw "Caddyfile validation failed with exit code $LASTEXITCODE" }

$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "Service $ServiceName already exists; recreating it"
    if ($existing.Status -ne 'Stopped') { Stop-Service -Name $ServiceName -Force }
    & $nssm remove $ServiceName confirm | Out-Null
}

if (-not $KeepRunningCaddy) { Stop-ManualCaddy -Port $AdminPort }

$arguments = "run --config `"$configPath`" --adapter caddyfile"
Write-Host "Installing $ServiceName -> $caddy $arguments"
& $nssm install $ServiceName $caddy $arguments | Out-Null

& $nssm set $ServiceName AppDirectory   $configDirectory | Out-Null
& $nssm set $ServiceName DisplayName    $DisplayName | Out-Null
& $nssm set $ServiceName Description    'Caddy web server for caddy-dev-local' | Out-Null
& $nssm set $ServiceName Start           SERVICE_AUTO_START | Out-Null
& $nssm set $ServiceName AppExit Default Restart | Out-Null
& $nssm set $ServiceName AppRestartDelay 5000 | Out-Null
& $nssm set $ServiceName AppStdout       $stdoutLog | Out-Null
& $nssm set $ServiceName AppStderr       $stderrLog | Out-Null
& $nssm set $ServiceName AppRotateFiles  10485760 | Out-Null
& $nssm set $ServiceName AppRotateOnline 1 | Out-Null

Write-Host "Starting $ServiceName"
Start-Service -Name $ServiceName

if (-not (Wait-ForAdminApi -Port $AdminPort)) {
    Write-Warning "Caddy did not answer on admin port $AdminPort within 30s."
    Write-Warning "Inspect $stderrLog and run: nssm status $ServiceName"
    exit 1
}

Write-Host ''
Write-Host "Installed $ServiceName"
Get-Service -Name $ServiceName | Format-Table Name, Status, StartType -AutoSize
Write-Host "Admin API:  http://localhost:$AdminPort/config/"
Write-Host "HTTP site:  http://localhost:$HttpPort/"
Write-Host "Logs:       $stdoutLog"
Write-Host "            $stderrLog"
Write-Host ''
Write-Host 'Next: start the devlocal watcher with `devlocal start` so managed routes are'
Write-Host 're-registered, since Caddy starts from the Caddyfile alone.'