//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cedar2025/xboard-node/internal/fileutil"
)

// Windows keeps the running xbctl.exe locked. A detached PowerShell helper
// waits for this process to exit, replaces both binaries, and starts the
// service only after the replacement is complete.
func scheduleWindowsUpgrade(newBinary, newCLI, version string) error {
	script := filepath.Join(os.TempDir(), fmt.Sprintf("xboard-node-upgrade-%d.ps1", os.Getpid()))
	content := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$parentPid = %d
$node = '%s'
$cli = '%s'
$newNode = '%s'
$newCli = '%s'
$nodeBak = "$node.bak"
$cliBak = "$cli.bak"
$service = '%s'
$meta = '%s'
$release = '%s'

while (Get-Process -Id $parentPid -ErrorAction SilentlyContinue) {
    Start-Sleep -Milliseconds 250
}

try {
    if (Test-Path -LiteralPath $node) { Copy-Item -LiteralPath $node -Destination $nodeBak -Force }
    if (Test-Path -LiteralPath $cli) { Copy-Item -LiteralPath $cli -Destination $cliBak -Force }
    Move-Item -LiteralPath $newNode -Destination $node -Force
    Move-Item -LiteralPath $newCli -Destination $cli -Force
    Start-Service -Name $service
    $deadline = (Get-Date).AddSeconds(30)
    do {
        $state = (Get-Service -Name $service).Status
        if ($state -eq 'Running') { break }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    if ((Get-Service -Name $service).Status -ne 'Running') {
        throw 'service did not reach Running after upgrade'
    }
    if (Test-Path -LiteralPath $meta) {
        try {
            $json = Get-Content -LiteralPath $meta -Raw | ConvertFrom-Json
            $json.version = (& $node -v | Out-String).Trim()
            $json.updated_at = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
            $json | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $meta -Encoding UTF8
        } catch { }
    }
    Remove-Item -LiteralPath $nodeBak, $cliBak -Force -ErrorAction SilentlyContinue
} catch {
    if (Test-Path -LiteralPath $nodeBak) { Move-Item -LiteralPath $nodeBak -Destination $node -Force }
    if (Test-Path -LiteralPath $cliBak) { Move-Item -LiteralPath $cliBak -Destination $cli -Force }
    try { Start-Service -Name $service } catch { }
    throw
} finally {
    Remove-Item -LiteralPath $newNode, $newCli, $nodeBak, $cliBak, '%s' -Force -ErrorAction SilentlyContinue
}
`, os.Getpid(), psQuote(defaultBinaryPath), psQuote(defaultCLIPath), psQuote(newBinary), psQuote(newCLI),
		psQuote(serviceName), psQuote(defaultMetaPath), psQuote(version), psQuote(script))
	if err := fileutil.WriteAtomic(script, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write Windows upgrade helper: %w", err)
	}
	if err := startDetachedPowerShell(script); err != nil {
		_ = os.Remove(script)
		return err
	}
	fmt.Println("Upgrade scheduled; the Windows Service will restart after xbctl exits")
	return nil
}

func scheduleWindowsUninstall(purge bool) error {
	script := filepath.Join(os.TempDir(), fmt.Sprintf("xboard-node-uninstall-%d.ps1", os.Getpid()))
	purgeLiteral := "$false"
	if purge {
		purgeLiteral = "$true"
	}
	content := fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$parentPid = %d
$service = '%s'
$node = '%s'
$cli = '%s'
$root = '%s'
$meta = '%s'
$purge = %s
while (Get-Process -Id $parentPid -ErrorAction SilentlyContinue) {
    Start-Sleep -Milliseconds 250
}
try {
    Stop-Service -Name $service -Force -ErrorAction SilentlyContinue
    $deadline = (Get-Date).AddSeconds(30)
    while ((Get-Service -Name $service -ErrorAction SilentlyContinue).Status -ne 'Stopped' -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 250
    }
    sc.exe delete $service | Out-Null
    Remove-Item -LiteralPath $node, $cli -Force -ErrorAction SilentlyContinue
    if ($purge) { Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue }
    else { Remove-Item -LiteralPath $meta -Force -ErrorAction SilentlyContinue }
} finally {
    Remove-Item -LiteralPath '%s' -Force -ErrorAction SilentlyContinue
}
`, os.Getpid(), psQuote(serviceName), psQuote(defaultBinaryPath), psQuote(defaultCLIPath),
		psQuote(defaultInstallRoot), psQuote(defaultMetaPath), purgeLiteral, psQuote(script))
	if err := fileutil.WriteAtomic(script, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write Windows uninstall helper: %w", err)
	}
	if err := startDetachedPowerShell(script); err != nil {
		_ = os.Remove(script)
		return err
	}
	fmt.Println("Uninstall scheduled; files will be removed after xbctl exits")
	return nil
}

func psQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func startDetachedPowerShell(script string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", script)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start detached PowerShell helper: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("detach PowerShell helper: %w", err)
	}
	return nil
}
