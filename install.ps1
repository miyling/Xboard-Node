#requires -RunAsAdministrator

[CmdletBinding()]
param(
    [ValidateSet('install', 'upgrade', 'uninstall', 'status')]
    [string]$Action = 'install',
    [ValidateSet('node', 'machine')]
    [string]$Mode = 'node',
    [string]$Panel,
    [string]$Token,
    [int]$NodeId,
    [string]$NodeType,
    [int]$MachineId,
    [ValidateSet('singbox', 'xray')]
    [string]$Kernel = 'singbox',
    [string]$Version = 'latest',
    [string]$Binary,
    [string]$XbctlBinary,
    [int]$HealthPort = 65530,
    [string]$Gomemlimit,
    [int]$Gogc,
    [switch]$Purge,
    [switch]$Yes
)

$ErrorActionPreference = 'Stop'

$appName = 'xboard-node'
$serviceName = 'xboard-node'
$releaseBase = 'https://github.com/miyling/Xboard-Node/releases'
$installRoot = Join-Path $env:ProgramData 'xboard-node'
$binRoot = Join-Path $env:ProgramFiles 'Xboard Node\bin'
$configPath = Join-Path $installRoot 'config.yml'
$credentialsPath = Join-Path $installRoot 'credentials.env'
$metaPath = Join-Path $installRoot 'install-meta.json'
$logPath = Join-Path $installRoot 'logs\xboard-node.log'
$nodePath = Join-Path $binRoot 'xboard-node.exe'
$cliPath = Join-Path $binRoot 'xbctl.exe'

function Write-Info([string]$Message) { Write-Host "[INFO] $Message" -ForegroundColor Green }
function Write-Warn([string]$Message) { Write-Host "[WARN] $Message" -ForegroundColor Yellow }

function Get-Architecture {
    if ($env:PROCESSOR_ARCHITEW6432) { $raw = $env:PROCESSOR_ARCHITEW6432 } else { $raw = $env:PROCESSOR_ARCHITECTURE }
    switch ($raw.ToUpperInvariant()) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "Unsupported Windows architecture: $raw" }
    }
}

function Resolve-Artifact([string]$Kind, [string]$Arch) {
    return "$Kind-windows-$Arch.exe"
}

function Resolve-DownloadUrl([string]$Artifact) {
    if ($Version -eq 'latest') { return "$releaseBase/latest/download/$Artifact" }
    return "$releaseBase/download/$Version/$Artifact"
}

function Ensure-Directories {
    New-Item -ItemType Directory -Force -Path $installRoot, $binRoot, (Split-Path -Parent $logPath) | Out-Null
}

function Copy-LocalArtifact([string]$Source, [string]$Destination) {
    if ($Source) {
        if (-not (Test-Path -LiteralPath $Source -PathType Leaf)) { throw "Binary source not found: $Source" }
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
    }
}

function Invoke-ArtifactDownloads([object[]]$Artifacts, [string]$Stage) {
    $remoteArtifacts = New-Object System.Collections.ArrayList
    foreach ($artifact in $Artifacts) {
        if ($artifact.Source) {
            Copy-LocalArtifact $artifact.Source $artifact.Destination
            continue
        }
        $artifact.Url = Resolve-DownloadUrl $artifact.Name
        [void]$remoteArtifacts.Add($artifact)
    }
    if ($remoteArtifacts.Count -eq 0) { return }

    $aria2 = Get-Command -Name 'aria2c.exe' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($aria2) {
        $aria2Path = $aria2.Source
        if (-not $aria2Path) { $aria2Path = $aria2.Path }
        if (-not $aria2Path) { $aria2Path = $aria2.Name }

        $processes = New-Object System.Collections.ArrayList
        $failureMessage = $null
        $failureArtifact = $null
        try {
            foreach ($artifact in $remoteArtifacts) {
                try {
                    Write-Info "Downloading $($artifact.Url)"
                    $arguments = @(
                        '--allow-overwrite=true',
                        '--auto-file-renaming=false',
                        '--split=8',
                        '--max-connection-per-server=8',
                        "--out=$([System.IO.Path]::GetFileName($artifact.Destination))",
                        $artifact.Url
                    )
                    $process = Start-Process -FilePath $aria2Path -WorkingDirectory $Stage -ArgumentList $arguments -PassThru -NoNewWindow
                    [void]$processes.Add([pscustomobject]@{ Artifact = $artifact; Process = $process })
                } catch {
                    if (-not $failureMessage) {
                        $failureMessage = $_.Exception.Message
                        $failureArtifact = $artifact.Name
                    }
                }
            }

            foreach ($job in $processes) {
                try {
                    $job.Process.WaitForExit()
                    if ($job.Process.ExitCode -ne 0) {
                        throw "aria2c exited with code $($job.Process.ExitCode)"
                    }
                    if (-not (Test-Path -LiteralPath $job.Artifact.Destination -PathType Leaf)) {
                        throw 'aria2c completed without producing the artifact'
                    }
                } catch {
                    if (-not $failureMessage) {
                        $failureMessage = $_.Exception.Message
                        $failureArtifact = $job.Artifact.Name
                    }
                }
            }

            if ($failureMessage) {
                throw "Download failed for $($failureArtifact): $failureMessage"
            }
        } finally {
            foreach ($job in $processes) {
                try {
                    if (-not $job.Process.HasExited) {
                        $job.Process.Kill()
                        $job.Process.WaitForExit()
                    }
                } catch { }
                try { $job.Process.Dispose() } catch { }
            }
        }
        return
    }

    $jobs = New-Object System.Collections.ArrayList
    $failureMessage = $null
    $failureArtifact = $null
    try {
        foreach ($artifact in $remoteArtifacts) {
            $client = $null
            try {
                Write-Info "Downloading $($artifact.Url)"
                $client = New-Object System.Net.WebClient
                $task = $client.DownloadFileTaskAsync([Uri]$artifact.Url, [string]$artifact.Destination)
                [void]$jobs.Add([pscustomobject]@{ Artifact = $artifact; Client = $client; Task = $task })
                $client = $null
            } catch {
                if ($client) {
                    try { $client.Dispose() } catch { }
                }
                if (-not $failureMessage) {
                    $failureMessage = $_.Exception.Message
                    $failureArtifact = $artifact.Name
                }
            }
        }

        foreach ($job in $jobs) {
            try {
                $job.Task.GetAwaiter().GetResult()
                if (-not (Test-Path -LiteralPath $job.Artifact.Destination -PathType Leaf)) {
                    throw 'Download completed without producing the artifact'
                }
            } catch {
                if (-not $failureMessage) {
                    $failureMessage = $_.Exception.Message
                    $failureArtifact = $job.Artifact.Name
                }
            }
        }

        if ($failureMessage) {
            throw "Download failed for $($failureArtifact): $failureMessage"
        }
    } finally {
        foreach ($job in $jobs) {
            try {
                if (-not $job.Task.IsCompleted) { $job.Client.CancelAsync() }
            } catch { }
        }
        foreach ($job in $jobs) {
            try {
                if (-not $job.Task.IsCompleted) { $job.Task.Wait(1000) | Out-Null }
            } catch { }
            try { $job.Client.Dispose() } catch { }
        }
    }
}

function Invoke-Xbctl([string]$Executable, [string[]]$Arguments) {
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) { throw "xbctl command failed with exit code $LASTEXITCODE" }
}

function Stop-ServiceIfPresent {
    $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if ($service -and $service.Status -ne 'Stopped') {
        Invoke-Xbctl $cliPath @('service', 'stop')
    }
}

function Install-Service {
    $existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if ($existing) {
        Stop-ServiceIfPresent
        Invoke-Xbctl $cliPath @('service', 'uninstall')
    }
    Invoke-Xbctl $cliPath @('service', 'install')
    Invoke-Xbctl $cliPath @('service', 'enable')
    Invoke-Xbctl $cliPath @('service', 'start')
}

function Wait-Healthy {
    if ($HealthPort -le 0) { return }
    $url = "http://127.0.0.1:$HealthPort/healthz"
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        try {
            $response = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 3
            if ($response.StatusCode -eq 200) { return }
        } catch { }
        Start-Sleep -Seconds 1
    }
    throw "xboard-node did not become healthy at $url"
}

function Show-StartupDiagnostics {
    if (-not (Test-Path -LiteralPath $logPath -PathType Leaf)) {
        Write-Warn "Startup log not found: $logPath"
        return
    }
    try {
        Write-Warn "Recent xboard-node startup log ($logPath):"
        $lines = @(Get-Content -LiteralPath $logPath -Tail 80 -ErrorAction Stop)
        $secrets = @()
        if (Test-Path -LiteralPath $credentialsPath -PathType Leaf) {
            foreach ($line in (Get-Content -LiteralPath $credentialsPath -ErrorAction SilentlyContinue)) {
                if ($line -match '^\s*[^#=][^=]*=(.*)$') {
                    $value = $Matches[1].Trim().Trim("'").Trim('"')
                    if ($value) { $secrets += $value }
                }
            }
        }
        foreach ($line in $lines) {
            foreach ($secret in $secrets) {
                $line = $line.Replace($secret, '<redacted>')
            }
            Write-Host "  $line"
        }
    } catch {
        Write-Warn "Could not read startup log: $($_.Exception.Message)"
    }
}

function Backup-State {
    $backupRoot = Join-Path $installRoot ('backups\' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
    New-Item -ItemType Directory -Force -Path $backupRoot | Out-Null
    foreach ($item in @(
        @{ Source = $nodePath; Name = 'xboard-node.exe' },
        @{ Source = $cliPath; Name = 'xbctl.exe' },
        @{ Source = $configPath; Name = 'config.yml' },
        @{ Source = $credentialsPath; Name = 'credentials.env' },
        @{ Source = $metaPath; Name = 'install-meta.json' }
    )) {
        if (Test-Path -LiteralPath $item.Source -PathType Leaf) {
            Copy-Item -LiteralPath $item.Source -Destination (Join-Path $backupRoot $item.Name) -Force
        }
    }
    return $backupRoot
}

function Restore-State([string]$BackupRoot) {
    foreach ($item in @(
        @{ Target = $nodePath; Name = 'xboard-node.exe' },
        @{ Target = $cliPath; Name = 'xbctl.exe' },
        @{ Target = $configPath; Name = 'config.yml' },
        @{ Target = $credentialsPath; Name = 'credentials.env' },
        @{ Target = $metaPath; Name = 'install-meta.json' }
    )) {
        $source = Join-Path $BackupRoot $item.Name
        if (Test-Path -LiteralPath $source -PathType Leaf) {
            Copy-Item -LiteralPath $source -Destination $item.Target -Force
        } else {
            Remove-Item -LiteralPath $item.Target -Force -ErrorAction SilentlyContinue
        }
    }
    Protect-Credentials
}

function Prepare-Configuration([string]$StagedCli, [string]$StagedConfig, [string]$StagedCredentials, [string]$StagedMeta) {
    $hasConfig = Test-Path -LiteralPath $configPath -PathType Leaf
    if ($hasConfig -and -not $Panel) {
        Copy-Item -LiteralPath $configPath -Destination $StagedConfig -Force
        if (Test-Path -LiteralPath $credentialsPath -PathType Leaf) { Copy-Item -LiteralPath $credentialsPath -Destination $StagedCredentials -Force }
        if (Test-Path -LiteralPath $metaPath -PathType Leaf) { Copy-Item -LiteralPath $metaPath -Destination $StagedMeta -Force }
        return
    }
    if (-not $Panel -or -not $Token) { throw 'A fresh install requires -Panel, -Token, and either -NodeId or -MachineId' }
    if ($Mode -eq 'node' -and $NodeId -le 0) { throw '-NodeId is required for node mode' }
    if ($Mode -eq 'machine' -and $MachineId -le 0) { throw '-MachineId is required for machine mode' }

    $init = @(
        'config', 'init', '--mode', $Mode, '--panel-url', $Panel,
        '--kernel', $Kernel, '--health-port', $HealthPort.ToString(),
        '--output', $StagedConfig, '--credentials-in', $credentialsPath,
        '--credentials-out', $StagedCredentials, '--meta', $StagedMeta,
        '--install-root', $installRoot, '--log-output', $logPath,
        '--version', $Version
    )
    if ($Token) { $init += @('--token', $Token) }
    if ($Mode -eq 'node') { $init += @('--node-id', $NodeId.ToString()) } else { $init += @('--machine-id', $MachineId.ToString()) }
    if ($NodeType) { $init += @('--node-type', $NodeType) }
    if ($Gomemlimit) { $init += @('--gomemlimit', $Gomemlimit) }
    if ($Gogc -gt 0) { $init += @('--gogc', $Gogc.ToString()) }
    Invoke-Xbctl $StagedCli $init
}

function Protect-Credentials {
    if (Test-Path -LiteralPath $credentialsPath -PathType Leaf) {
        & icacls.exe $credentialsPath /inheritance:r /grant:r '*S-1-5-18:(F)' '*S-1-5-32-544:(F)' | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "Could not protect credentials file: $credentialsPath" }
    }
}

function Set-HealthPortFromConfig([string]$ConfigFile, [string]$Cli) {
    try {
        $value = (& $Cli config health-port --config $ConfigFile 2>$null | Select-Object -First 1).ToString().Trim()
        if ($value -match '^[0-9]+$') { $script:HealthPort = [int]$value }
    } catch { }
}

function Install-OrUpgrade {
    Ensure-Directories
    $arch = Get-Architecture
    $stage = Join-Path $env:TEMP ("xboard-node-stage-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $stage | Out-Null
    try {
        $stagedNode = Join-Path $stage 'xboard-node.exe'
        $stagedCli = Join-Path $stage 'xbctl.exe'
        $stagedConfig = Join-Path $stage 'config.yml'
        $stagedCredentials = Join-Path $stage 'credentials.env'
        $stagedMeta = Join-Path $stage 'install-meta.json'
        $artifacts = @(
            [pscustomobject]@{
                Name = Resolve-Artifact 'xboard-node' $arch
                Source = $Binary
                Destination = $stagedNode
                Url = $null
            },
            [pscustomobject]@{
                Name = Resolve-Artifact 'xbctl' $arch
                Source = $XbctlBinary
                Destination = $stagedCli
                Url = $null
            }
        )
        Invoke-ArtifactDownloads $artifacts $stage
        Prepare-Configuration $stagedCli $stagedConfig $stagedCredentials $stagedMeta
        $hadService = $null -ne (Get-Service -Name $serviceName -ErrorAction SilentlyContinue)
        $backup = Backup-State
        try {
            if ($hadService) {
                Stop-ServiceIfPresent
                Invoke-Xbctl $cliPath @('service', 'uninstall')
                for ($attempt = 0; $attempt -lt 30 -and (Get-Service -Name $serviceName -ErrorAction SilentlyContinue); $attempt++) {
                    Start-Sleep -Milliseconds 250
                }
            }
            Copy-Item -LiteralPath $stagedNode -Destination $nodePath -Force
            Copy-Item -LiteralPath $stagedCli -Destination $cliPath -Force
            Copy-Item -LiteralPath $stagedConfig -Destination $configPath -Force
            if (Test-Path -LiteralPath $stagedCredentials) { Copy-Item -LiteralPath $stagedCredentials -Destination $credentialsPath -Force }
            if (Test-Path -LiteralPath $stagedMeta) { Copy-Item -LiteralPath $stagedMeta -Destination $metaPath -Force }
            Protect-Credentials
            Set-HealthPortFromConfig $configPath $stagedCli
            Install-Service
            Wait-Healthy
        } catch {
            Write-Warn "Installation failed; restoring the previous installation"
            Show-StartupDiagnostics
            try {
                if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
                    Stop-ServiceIfPresent
                    Invoke-Xbctl $cliPath @('service', 'uninstall')
                }
            } catch { Write-Warn "Could not remove failed service: $($_.Exception.Message)" }
            for ($attempt = 0; $attempt -lt 30 -and (Get-Service -Name $serviceName -ErrorAction SilentlyContinue); $attempt++) {
                Start-Sleep -Milliseconds 250
            }
            Restore-State $backup
            if ($hadService -and (Test-Path -LiteralPath $cliPath -PathType Leaf)) {
                try {
                    Invoke-Xbctl $cliPath @('service', 'install')
                    Invoke-Xbctl $cliPath @('service', 'enable')
                    Invoke-Xbctl $cliPath @('service', 'start')
                } catch { Write-Warn "Could not restart the previous service: $($_.Exception.Message)" }
            }
            throw
        }
        Write-Info "Installation succeeded (backup: $backup)"
        Write-Info "Service: $serviceName"
        Write-Info "Config: $configPath"
        Write-Info "Credentials: $credentialsPath"
        Write-Info "CLI: $cliPath"
    } finally {
        Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
    }
}

function Uninstall-Node {
    if (-not $Yes) {
        $answer = Read-Host 'Proceed with uninstall? [y/N]'
        if ($answer -notmatch '^[Yy]$') { Write-Warn 'Uninstall cancelled'; return }
    }
    if (Test-Path -LiteralPath $cliPath -PathType Leaf) {
        try { Invoke-Xbctl $cliPath @('service', 'uninstall') } catch { Write-Warn $_.Exception.Message }
    }
    Remove-Item -LiteralPath $nodePath, $cliPath -Force -ErrorAction SilentlyContinue
    if ($Purge) {
        Remove-Item -LiteralPath $installRoot -Recurse -Force -ErrorAction SilentlyContinue
        Write-Info "Removed $installRoot"
    } else {
        Remove-Item -LiteralPath $metaPath -Force -ErrorAction SilentlyContinue
        Write-Info "Config preserved under $installRoot"
    }
    Write-Info 'Uninstall complete'
}

switch ($Action) {
    'status' {
        $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
        if ($service) { $service | Format-List Name, Status, StartType } else { Write-Host 'xboard-node: not installed' }
    }
    'install' { Install-OrUpgrade }
    'upgrade' { Install-OrUpgrade }
    'uninstall' { Uninstall-Node }
}
