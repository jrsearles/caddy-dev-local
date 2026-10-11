#requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string] $ServiceName = 'DevLocalCaddy',
    [string] $DisplayName = 'Caddy (devlocal)',
    [string] $DataDirectory = "$env:ProgramData\DevLocal",
    [string] $CaddyExe = '',
    [string] $NssmExe = '',
    [int] $AdminPort = 2019,
    [int] $HttpPort = 80,
    [switch] $SkipManualCaddyStop
)

$ErrorActionPreference = 'Stop'

function Write-Step {
    param([string] $Message)

    Write-Host ''
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Write-Ok {
    param([string] $Message)

    Write-Host "  $Message" -ForegroundColor Green
}

function Get-Exe {
    param(
        [string] $Path,
        [string] $Name,
        [string] $InstallHint
    )

    if ($Path -and $Path.Trim()) {
        if (Test-Path -LiteralPath $Path) {
            return (Resolve-Path -LiteralPath $Path).Path
        }
        throw "$Name not found at '$Path'."
    }

    $command = Get-Command $Name -ErrorAction Ignore
    if ($command) { return $command.Source }

    throw "$Name not found in PATH. $InstallHint"
}

function Stop-ManualCaddy {
    param([int] $Port)

    $listeners = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    $pids = @($listeners | Select-Object -ExpandProperty OwningProcess -Unique)
    if ($pids.Count -eq 0) {
        Write-Ok "Port $Port is already free"
        return
    }

    foreach ($processId in $pids) {
        $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
        if (-not $process) { continue }
        if ($process.ProcessName -ne 'caddy') {
            throw "Port $Port is held by $($process.ProcessName) (PID $processId). Stop it and re-run, or pass a different -AdminPort."
        }
        Write-Host "Stopping the manually started caddy (PID $processId) that holds port $Port"
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
	storage file_system {
		root $Storage
	}
}

http://localhost:$Http {
	respond "caddy-dev-local placeholder"
}
"@
    Set-Content -LiteralPath $Path -Value $content -Encoding utf8NoBOM
    Write-Ok "Wrote a default Caddyfile to $Path"
}

function Wait-ForAdminApi {
    param([int] $Port, [int] $TimeoutSeconds = 30)

    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        try {
            $response = Invoke-WebRequest -Uri "http://localhost:$Port/config/" -TimeoutSec 3
            if ($response.StatusCode -eq 200) {
                Write-Host ''
                return $true
            }
        }
        catch { }
        Write-Host '.' -NoNewline
        Start-Sleep -Milliseconds 500
    }
    return $false
}

Write-Host "Installing Caddy as the '$DisplayName' Windows service:"
Write-Host "  admin API on port $AdminPort, HTTP on port $HttpPort, data under $DataDirectory"

Write-Step 'Locating caddy and nssm'
$caddy = Get-Exe -Path $CaddyExe -Name 'caddy.exe' -InstallHint 'Install Caddy (choco install caddy) or pass its path with -CaddyExe.'
$nssm = Get-Exe -Path $NssmExe -Name 'nssm.exe' -InstallHint 'Install NSSM (choco install nssm) or pass its path with -NssmExe.'
Write-Ok "caddy: $caddy"
Write-Ok "nssm:  $nssm"

Write-Step "Preparing $DataDirectory"
$configPath = Join-Path $DataDirectory 'Caddyfile'
$storageDirectory = Join-Path $DataDirectory 'caddy-data'
$stdoutLog = Join-Path $DataDirectory 'caddy.log'
$stderrLog = Join-Path $DataDirectory 'caddy.err.log'

foreach ($directory in @($DataDirectory, $storageDirectory)) {
    if (-not (Test-Path -LiteralPath $directory)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
        Write-Ok "Created $directory"
    }
}

if (Test-Path -LiteralPath $configPath) {
    Write-Host "Found an existing Caddyfile at $configPath"
    $answer = Read-Host 'Keep it and its settings? [Y/n]'
    if ($answer -match '^(n|no)$') {
        Write-DefaultCaddyfile -Path $configPath -Admin $AdminPort -Http $HttpPort -Storage $storageDirectory
    }
    else {
        Write-Ok "Keeping $configPath as-is"
    }
}
else {
    Write-DefaultCaddyfile -Path $configPath -Admin $AdminPort -Http $HttpPort -Storage $storageDirectory
}

Write-Step 'Validating the Caddyfile'
$validation = & $caddy validate --config $configPath --adapter caddyfile 2>&1
if ($LASTEXITCODE -ne 0) {
    foreach ($line in @($validation)) { Write-Host "  $line" -ForegroundColor Red }
    throw "The Caddyfile is invalid. Fix the errors above, or delete $configPath and re-run to regenerate the default."
}
Write-Ok 'The Caddyfile is valid'

Write-Step 'Removing the previous service and any manually started caddy'
$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing) {
    if ($existing.Status -ne 'Stopped') {
        Stop-Service -Name $ServiceName -Force
        Write-Ok "Stopped the previous $ServiceName"
    }
    & $nssm remove $ServiceName confirm | Out-Null
    Write-Ok "Removed the previous $ServiceName"
}
else {
    Write-Ok "No previous $ServiceName found"
}

if ($SkipManualCaddyStop) {
    Write-Warning "Skipping the check for a manually started caddy; the service cannot start while port $AdminPort is taken."
}
else {
    Stop-ManualCaddy -Port $AdminPort
}

Write-Step "Installing and configuring $ServiceName"
$arguments = "run --config `"$configPath`" --adapter caddyfile"
& $nssm install $ServiceName $caddy $arguments | Out-Null

$settings = [ordered]@{
    AppDirectory    = $DataDirectory
    DisplayName     = $DisplayName
    Description     = 'Caddy web server for caddy-dev-local'
    Start           = 'SERVICE_AUTO_START'
    AppRestartDelay = 5000
    AppStdout       = $stdoutLog
    AppStderr       = $stderrLog
    AppRotateFiles  = 1
    AppRotateBytes  = 10485760
    AppRotateOnline = 1
}
foreach ($setting in $settings.GetEnumerator()) {
    & $nssm set $ServiceName $setting.Key $setting.Value | Out-Null
}
& $nssm set $ServiceName AppExit Default Restart | Out-Null
Write-Ok 'Auto-start at boot, restart after crashes, rotate logs at 10 MB'

Write-Step "Starting $ServiceName and waiting for its admin API"
Start-Service -Name $ServiceName

Write-Host 'Waiting for the admin API ' -NoNewline
if (-not (Wait-ForAdminApi -Port $AdminPort)) {
    Write-Host ''
    Write-Host "Caddy did not answer on the admin API (port $AdminPort) within 30s." -ForegroundColor Red
    if (Test-Path -LiteralPath $stderrLog) {
        Write-Host "Last lines of $stderrLog"
        foreach ($line in (Get-Content -LiteralPath $stderrLog -Tail 15)) { Write-Host "  $line" -ForegroundColor DarkGray }
    }
    throw "Check the output above and 'nssm status $ServiceName', then re-run this script."
}
Write-Ok "The admin API is answering on http://localhost:$AdminPort/config/"

Write-Step 'Trusting the Caddy root CA (so browsers accept https://*.dev.local)'
& $caddy trust --config $configPath
if ($LASTEXITCODE -ne 0) {
    Write-Warning 'Trusting the root CA failed; browsers may reject https://*.dev.local until it succeeds.'
    Write-Warning "Retry later with: & `"$caddy`" trust --config `"$configPath`""
}
else {
    Write-Ok 'The root CA is trusted'
}

Write-Host ''
Write-Host "Installed $ServiceName." -ForegroundColor Green
$service = Get-Service -Name $ServiceName
Write-Host "  Status:    $($service.Status) (auto-starts at boot)"
Write-Host "  Admin API: http://localhost:$AdminPort/config/"
Write-Host "  HTTP site: http://localhost:$HttpPort/"
Write-Host "  Caddyfile: $configPath"
Write-Host "  Logs:      $stdoutLog and $stderrLog"
Write-Host ''
Write-Host 'Next: run `devlocal start` so devlocal re-registers its routes with Caddy.'
