#requires -RunAsAdministrator

[CmdletBinding()]
param(
    [string]$BinDir,
    [int]$HealthPort = 0
)

$ErrorActionPreference = 'Stop'
if (-not $BinDir) { $BinDir = Join-Path $env:ProgramFiles 'Xboard Node\bin' }
$node = Join-Path $BinDir 'xboard-node.exe'
$cli = Join-Path $BinDir 'xbctl.exe'
$serviceName = 'xboard-node'

foreach ($path in @($node, $cli)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Missing executable: $path" }
}

Write-Host (& $node -v)
& $cli version
if ($LASTEXITCODE -ne 0) { throw 'xbctl version failed' }

$service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if (-not $service) { throw "Windows service $serviceName is not installed" }

& $cli service start
if ($LASTEXITCODE -ne 0) { throw 'service start failed' }

if ($HealthPort -gt 0) {
    $url = "http://127.0.0.1:$HealthPort/healthz"
    $healthy = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        try {
            if ((Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 3).StatusCode -eq 200) {
                $healthy = $true
                break
            }
        } catch { }
        Start-Sleep -Seconds 1
    }
    if (-not $healthy) { throw "health check failed: $url" }
}

& $cli status
if ($LASTEXITCODE -ne 0) { throw 'xbctl status failed' }

& $cli service stop
if ($LASTEXITCODE -ne 0) { throw 'service stop failed' }
$service = Get-Service -Name $serviceName
if ($service.Status -ne 'Stopped') { throw "controlled stop ended in $($service.Status)" }

Write-Host 'Windows Server xboard-node smoke checks passed.' -ForegroundColor Green
