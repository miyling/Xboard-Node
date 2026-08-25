# aria2 and PowerShell 5.1 download strategy

## Repository evidence

- `install.ps1` currently downloads the node executable and `xbctl.exe`
  sequentially through `Invoke-WebRequest` in `Install-OrUpgrade`.
- The two remote artifact names and URLs are derived from the selected Windows
  architecture and `$Version`; neither should change as part of this task.
- The script already stages both files under a unique directory and removes the
  directory in `finally`, so the download layer can keep using those paths.
- No aria2 binary, package-manager bootstrap, or installer dependency exists in
  the repository.

## Selected behavior

`aria2c.exe` is optional. When it is available through `PATH`, one aria2 process
is started per remote artifact with its working directory set to the existing
staging directory. The process uses the staged destination filename as `--out`,
`--split=8`, and `--max-connection-per-server=8`; this gives both asset-level
parallelism and multiple HTTP connections for each asset without requiring a
new installer parameter.

When aria2 is absent, each remote artifact gets a .NET `System.Net.WebClient`
and `DownloadFileTaskAsync`. The tasks start before either task is awaited, so
Windows PowerShell 5.1 still downloads both assets concurrently without
PowerShell 7-only syntax or a third-party dependency.

Local `-Binary` and `-XbctlBinary` sources continue to use `Copy-Item`. A
failed aria2 process or download task is reported as an installation failure;
outstanding processes/tasks are cancelled or stopped in cleanup, and the
existing staging `finally` removes partial files.

## Compatibility notes

- `Get-Command aria2c.exe`, `Start-Process -PassThru`, and
  `System.Net.WebClient` are available to the Windows PowerShell 5.1 path used
  by the installer.
- The fallback avoids `ForEach-Object -Parallel`, `Start-ThreadJob`, and any
  package-manager bootstrap.
- aria2's proxy/TLS behavior can differ from `Invoke-WebRequest`; actual
  Windows Server validation should cover both the aria2-present and fallback
  branches. The current development environment has no `pwsh`, `powershell`,
  or `aria2c`, so that validation is deferred to a Windows host.
