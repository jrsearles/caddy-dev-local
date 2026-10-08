#requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string] $ServiceName = 'DevLocalCaddy',
    [string] $DataDirectory = "$env:ProgramData\DevLocal",
    [string] $CaddyExe = '',
    [string] $NssmExe = ''
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

function Confirm-Removal {
    param(
        [string] $Target,
        [string[]] $Notes = @()
    )

    foreach ($note in $Notes) {
        Write-Warning $note
    }
    $answer = Read-Host "Remove $Target? [y/N]"
    return $answer -match '^(y|yes)$'
}

function Get-StorageCertificates {
    param([string] $StorageDirectory)

    $pkiDirectory = Join-Path $StorageDirectory 'pki\authorities\local'
    if (-not (Test-Path -LiteralPath $pkiDirectory)) { return @() }

    $certificates = @()
    foreach ($fileName in @('root.crt', 'intermediate.crt')) {
        $certificatePath = Join-Path $pkiDirectory $fileName
        if (-not (Test-Path -LiteralPath $certificatePath)) { continue }
        $pem = (Get-Content -LiteralPath $certificatePath -Raw) -replace '\s', ''
        $base64 = $pem -replace '-----BEGINCERTIFICATE-----', '' -replace '-----ENDCERTIFICATE-----', ''
        $certificates += New-Object System.Security.Cryptography.X509Certificates.X509Certificate2 (, [Convert]::FromBase64String($base64))
    }
    return $certificates
}

function Remove-FromTrustStore {
    param(
        [System.Security.Cryptography.X509Certificates.X509Certificate2[]] $Certificates,
        [string] $StoreName
    )

    $store = New-Object System.Security.Cryptography.X509Certificates.X509Store($StoreName, 'LocalMachine')
    try {
        $store.Open([System.Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)
        foreach ($certificate in $Certificates) {
            $installed = @($store.Certificates | Where-Object { $_.Thumbprint -eq $certificate.Thumbprint })
            foreach ($entry in $installed) {
                $store.Remove($entry)
                Write-Ok "Removed $($entry.Subject) from the LocalMachine $StoreName store"
            }
        }
    }
    finally {
        $store.Close()
    }
}

function Untrust-RootCertificate {
    param(
        [string] $CaddyExe,
        [string] $CertificatePath,
        [System.Security.Cryptography.X509Certificates.X509Certificate2[]] $Certificates
    )

    $caddy = $null
    try { $caddy = Get-Exe -Path $CaddyExe -Name 'caddy.exe' -InstallHint '' }
    catch { }

    if ($caddy) {
        Write-Host "Running caddy untrust --cert $CertificatePath"
        try {
            & $caddy untrust --cert $CertificatePath
            if ($LASTEXITCODE -eq 0) { return }
            Write-Warning "caddy untrust failed with exit code $LASTEXITCODE; removing the certificates from the trust stores directly."
        }
        catch {
            Write-Warning "caddy untrust failed: $_; removing the certificates from the trust stores directly."
        }
    }
    else {
        Write-Warning 'caddy.exe not found; removing the certificates from the trust stores directly.'
    }

    Remove-FromTrustStore -Certificates $Certificates -StoreName 'Root'
    Remove-FromTrustStore -Certificates $Certificates -StoreName 'CA'
}

$configPath = Join-Path $DataDirectory 'Caddyfile'
$storageDirectory = Join-Path $DataDirectory 'caddy-data'
$stdoutLog = Join-Path $DataDirectory 'caddy.log'
$stderrLog = Join-Path $DataDirectory 'caddy.err.log'

Write-Host "This uninstalls the '$ServiceName' Caddy service. You choose what else to remove"
Write-Host '(root CA trust, logs, Caddyfile, cert storage); nothing is deleted without asking.'

Write-Step 'Removing the service'
$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $service) {
    Write-Ok "$ServiceName is not installed"
}
else {
    $nssm = Get-Exe -Path $NssmExe -Name 'nssm.exe' -InstallHint 'Install NSSM (choco install nssm) or pass its path with -NssmExe.'
    if ($service.Status -ne 'Stopped') {
        Stop-Service -Name $ServiceName -Force
        $service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30))
        Write-Ok "Stopped $ServiceName"
    }
    & $nssm remove $ServiceName confirm | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "nssm remove failed with exit code $LASTEXITCODE. Run 'nssm status $ServiceName' for details."
    }
    Write-Ok "Removed $ServiceName"
}

Write-Step 'Untrusting the Caddy root CA (optional)'
$storageCertificates = Get-StorageCertificates -StorageDirectory $storageDirectory
$rootCaStillTrusted = $false
if ($storageCertificates.Count -eq 0) {
    Write-Ok 'No Caddy root CA found on disk; nothing to untrust'
}
elseif (Confirm-Removal -Target 'the Caddy root CA from the local trust stores' -Notes @(
        'Every certificate it issued (all your .dev.local HTTPS certs) will fail validation until it is trusted again.')) {
    $rootCertificatePath = Join-Path $storageDirectory 'pki\authorities\local\root.crt'
    Untrust-RootCertificate -CaddyExe $CaddyExe -CertificatePath $rootCertificatePath -Certificates $storageCertificates
}
else {
    $rootCaStillTrusted = $true
}

Write-Step 'Removing Caddy files (optional)'
$existingLogs = @($stdoutLog, $stderrLog) | Where-Object { Test-Path -LiteralPath $_ }
if ($existingLogs.Count -eq 0) {
    Write-Ok 'No log files found'
}
else {
    foreach ($log in $existingLogs) {
        $sizeKb = [math]::Round((Get-Item -LiteralPath $log).Length / 1KB)
        Write-Host "  $log ($sizeKb kB)"
    }
    if (Confirm-Removal -Target 'these log files') {
        foreach ($log in $existingLogs) {
            Remove-Item -LiteralPath $log -Force
            Write-Ok "Removed $log"
        }
    }
}

if (Test-Path -LiteralPath $configPath) {
    if (Confirm-Removal -Target $configPath -Notes @('Keeping it preserves your configuration for a future install.')) {
        Remove-Item -LiteralPath $configPath -Force
        Write-Ok "Removed $configPath"
    }
}
else {
    Write-Ok 'No Caddyfile found'
}

if (Test-Path -LiteralPath $storageDirectory) {
    $notes = @(
        "$storageDirectory holds the Caddy root CA and every certificate it issued.",
        'Deleting it invalidates those certificates and any trust you granted the CA.'
    )
    if ($rootCaStillTrusted) { $notes += 'The root CA is still trusted; untrust it first (re-run this script) if you also want that gone.' }
    if (Confirm-Removal -Target $storageDirectory -Notes $notes) {
        Remove-Item -LiteralPath $storageDirectory -Recurse -Force
        Write-Ok "Removed $storageDirectory"
    }
}
else {
    Write-Ok 'No cert storage found'
}

Write-Step "Removing everything under $DataDirectory (optional)"
$notes = @(
    "This deletes everything in $DataDirectory, including devlocal settings, status and logs - not just Caddy files."
)
if ($rootCaStillTrusted) { $notes += 'The Caddy root CA would remain trusted.' }
if (-not (Test-Path -LiteralPath $DataDirectory)) {
    Write-Ok "$DataDirectory does not exist"
}
elseif (Confirm-Removal -Target $DataDirectory -Notes $notes) {
    Remove-Item -LiteralPath $DataDirectory -Recurse -Force
    Write-Ok "Removed $DataDirectory"
}

Write-Host ''
Write-Host "Uninstalled the $ServiceName service." -ForegroundColor Green

$kept = @(
    $stdoutLog,
    $stderrLog,
    $configPath,
    $storageDirectory,
    $DataDirectory
) | Where-Object { Test-Path -LiteralPath $_ }
if ($kept.Count -gt 0) {
    Write-Host 'Kept in place (re-run this script and answer y to remove):'
    foreach ($path in $kept) {
        Write-Host "  $path"
    }
}

if ($rootCaStillTrusted) {
    Write-Warning 'The Caddy root CA is still trusted. Re-run this script (or `caddy untrust`) to untrust it.'
}

Write-Host ''
Write-Host 'Tip: run `devlocal clean` to remove devlocal routes, TLS policies and hosts-file entries.'
