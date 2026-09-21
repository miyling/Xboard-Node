# Windows Server Deployment Contract

## 1. Scope / Trigger

This contract applies to changes that touch Windows Server execution, service
control, installers, credentials, platform paths, or cross-platform file
replacement. The node remains one Go binary with shared panel and kernel
behavior; only host and distribution boundaries are platform-specific.

## 2. Signatures

- `xboard-node.exe -c <config-path> [-credentials <credentials-path>]`
  starts the shared application. If `-credentials` is omitted, the loader
  reads `credentials.env` beside the config file.
- `config.LoadCredentialsFile(path string) error` loads dotenv-style values
  before `config.LoadRoot` resolves `token_env` references.
- `fileutil.WriteAtomic(path string, data []byte, perm os.FileMode) error`
  writes a complete same-directory temp file and performs a platform-native
  replacement.
- `fileutil.CopyAndReplace(dst string, src io.Reader, perm os.FileMode) error`
  provides the same replacement contract for large downloads.
- `fileutil.ProtectSecretFile(path string) error` reapplies private file
  permissions after an atomic credentials replacement.
- `monitor.CollectAt(dataRoot string) Status` reports disk usage from the
  instance data volume on Windows; Unix retains `/` as the default root.
- `xbctl service` accepts `install`, `uninstall`, `status`, `start`, `stop`,
  `restart`, `enable`, `disable`, and `logs`.
- Release artifacts are `xboard-node-windows-amd64.exe`,
  `xbctl-windows-amd64.exe`, with matching `arm64` variants.

## 3. Contracts

### Paths and service

- Install/config root: `%ProgramData%\xboard-node`.
- Binaries: `%ProgramFiles%\Xboard Node\bin\xboard-node.exe` and
  `%ProgramFiles%\Xboard Node\bin\xbctl.exe`.
- Config, credentials, and metadata: `config.yml`, `credentials.env`, and
  `install-meta.json` under the install root.
- Default log: `%ProgramData%\xboard-node\logs\xboard-node.log`.
- SCM service name: `xboard-node`.
- The service command line contains absolute config and credentials paths only;
  tokens must never be embedded in the SCM command line.
- Service start type is automatic. Recovery actions restart after 5, 15, and
  60 seconds; non-zero application exits are counted as failures, while a
  controlled stop reports `Stopped` with code zero and is not treated as a
  crash by the SCM.

### Startup state and diagnostics

- `xboard-node.exe` remains `StartPending` until credentials/config loading,
  startup-layout validation, health-listener setup, and managed-runtime launch
  have completed. Only then may the Windows host report `Running`.
- If the application exits before readiness, the host reports `Stopped` with
  a non-zero exit code. `xbctl service start` treats that state as terminal and
  includes both SCM exit codes plus the configured log path instead of waiting
  for the generic pending timeout.
- The host uses `<config-directory>\logs\xboard-node.log` as a best-effort
  append-only fallback when the configured logger is not available. It
  redacts values loaded from `credentials.env` and matching process
  environment variables.
- The PowerShell installer may print only a bounded, redacted tail of the
  startup log while entering rollback. A missing or locked log must never
  prevent service removal or restoration of the previous installation.

### Credentials

- The credentials file accepts UTF-8 BOM, CRLF, blank lines, `#` comments,
  `KEY=VALUE`, values containing `=`, and matching single/double quotes.
- Keys match `[A-Za-z_][A-Za-z0-9_]*`.
- Existing process environment variables take precedence over file values.
- Malformed lines fail startup with an error; secret values are not logged.
- The PowerShell installer restricts the generated file to SYSTEM and the
  built-in Administrators SID.
- `xbctl config init` and binding updates reapply the same protection after
  writing `credentials.env`; replacing a protected destination must not widen
  its access policy.

### Files and reload

- Certificate, GeoData, YAML, credentials, and metadata writes must use the
  shared replacement helper. Fixed `destination.tmp` names are forbidden.
- The watcher observes parent-directory `Write`, `Create`, `Rename`, and
  `Remove` events, debounces them, and retries a temporarily missing file
  caused by an atomic-save sequence.
- Reloading a file-backed logger closes the previously owned file handle;
  stdout and stderr are process-owned and remain open.

## 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Credentials file absent | Continue; explicit process environment remains valid. |
| Credentials line without `=` or with an invalid key | Return an error before config startup; do not partially apply the line. |
| Destination is briefly locked during replacement | Windows helper retries sharing/access errors, then returns a path-specific error. |
| Config is renamed away temporarily | Watcher retries after `Remove`/`Rename`; current running config remains active if reload never succeeds. |
| SCM sends Stop or Shutdown | Report `StopPending`, cancel the shared context, wait for node shutdown, then report `Stopped`. |
| Service exits before readiness | Write a redacted fallback-log entry, report non-zero `Stopped`, and make `xbctl service start` return the SCM exit codes and log path. |
| Node exits without a controlled stop | SCM recovery actions may restart it. |
| `xbctl.exe` upgrades itself | Detached PowerShell waits for the parent PID before replacing the CLI and node binaries. |
| Installer health check fails | Restore backed-up binaries/config/credentials/metadata and reinstall/start the prior service when one existed. |
| Disk root is unavailable to gopsutil | Leave disk values at zero and continue reporting other metrics. |

## 5. Good / Base / Bad Cases

- Good: SCM starts `xboard-node.exe -c C:\ProgramData\xboard-node\config.yml
  -credentials C:\ProgramData\xboard-node\credentials.env`; the loader reads
  the file and resolves `token_env` without a shell.
- Good: an editor saves `config.yml` by writing a sibling temp file and
  renaming it; the parent watcher reloads the complete file after debounce.
- Base: a manually launched console process omits `-credentials`; it loads the
  sibling file and still supports Ctrl+C debugging.
- Bad: putting `TOKEN=value` in the Windows Service `BinaryPathName`.
- Bad: using `disk.Usage("/")` on Windows or writing `config.yml.tmp` with a
  fixed name shared by concurrent writers.
- Bad: reporting `Running` immediately after spawning the application
  goroutine; a config or credential error then becomes an opaque SCM timeout.

## 6. Tests Required

- Unit test credentials parsing for BOM, CRLF, comments, quoted values,
  embedded `=`, explicit environment precedence, and malformed lines.
- Unit test atomic replacement leaves complete destination contents and no temp
  file; cross-compile the Windows implementation.
- Run `go test -race -count=1 ./internal/...` on Unix.
- Run `go vet ./...` and build `cmd/xboard-node`/`cmd/xbctl` for Windows
  `amd64` and `arm64` with release build tags.
- On a Windows runner, install into a path containing spaces, exercise service
  install/start/status/stop/uninstall, verify `/healthz`, and distinguish a
  controlled stop from a crash recovery restart.
- Run the PowerShell installer with a failed health check against a staged
  test binary and assert the backup state is restored.
- Test invalid credentials/config startup and assert `Stopped` is non-zero,
  `xbctl service start` returns without the full timeout, and neither the
  fallback log nor installer output contains the credential value.

## 7. Wrong vs Correct

### Wrong

```go
os.WriteFile(configPath+".tmp", data, 0o600)
os.Rename(configPath+".tmp", configPath)
```

This collides under concurrent writers and relies on Unix rename semantics.

### Correct

```go
return fileutil.WriteAtomic(configPath, data, 0o600)
```

The shared helper chooses a unique same-directory temp file and dispatches to
`os.Rename` on Unix or `MoveFileEx(...MOVEFILE_REPLACE_EXISTING...)` on
Windows.

For service startup, the corresponding correct boundary is:

```go
status <- svc.Status{State: svc.StartPending, WaitHint: 10_000}
go runApplicationWithReady(ctx, configPath, credentialsPath, signalReady)
// report svc.Running only from signalReady; report Stopped(non-zero) on error
```

## 8. Installer artifact download contract

### 1. Scope / Trigger

This contract applies when `install.ps1` or its upgrade path fetches the
independent `xboard-node.exe` and `xbctl.exe` release artifacts before changing
the installed service.

### 2. Signatures

- `-Binary` and `-XbctlBinary` remain optional local file overrides.
- Remote artifact names remain
  `xboard-node-windows-<arch>.exe` and `xbctl-windows-<arch>.exe`.
- `aria2c.exe` is preferably discovered through `PATH`; when absent, the
  temporary bootstrap contract in section 9 may provide a verified amd64
  executable. No new installer parameter or package-manager bootstrap is
  required.

### 3. Contracts

- If `aria2c.exe` is available, start one process per remote artifact with the
  existing staging directory as its working directory, `--split=8`,
  `--max-connection-per-server=8`, and the staged destination filename
  (`xboard-node.exe` or `xbctl.exe`) as `--out`; the Release asset name remains
  the URL's final artifact identifier.
- If system aria2 and the verified temporary bootstrap are unavailable, start
  one .NET `WebClient.DownloadFileTaskAsync` task per remote artifact before
  awaiting any task. This path must work in Windows PowerShell 5.1.
- Local overrides use `Copy-Item` and do not invoke a downloader.
- All downloads must finish successfully and produce regular files before
  configuration generation, backup, file replacement, or service operations
  begin.
- The existing unique staging directory owns partial artifacts; downloader
  processes/tasks and client handles must be stopped/cancelled and disposed on
  both success and failure.
- aria2 processes are started with `System.Diagnostics.Process` +
  `ProcessStartInfo` (`UseShellExecute = $false`), never with
  `Start-Process -PassThru`. `Start-Process` returns a `Process` object that the
  cmdlet did not spawn, so `ExitCode` reads back `$null` even after
  `WaitForExit()` and a successful download is misreported as a failure.
  `UseShellExecute = $false` with the default `CreateNoWindow` keeps the child
  attached to the current console, so aria2 progress stays visible.
- Arguments are joined into `ProcessStartInfo.Arguments` with quoting applied
  per argument; `Start-Process -ArgumentList` joins array elements with spaces
  without quoting, which corrupts any value containing whitespace.
- The exit code is read once into a variable and an unreadable (`$null`) exit
  code is reported as its own condition rather than interpolated into a
  non-zero-exit message.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| `aria2c.exe` absent | Try the pinned amd64 bootstrap; use concurrent .NET tasks for ARM64 or bootstrap failure without requiring package installation. |
| aria2 process exits non-zero | Report the artifact and exit code; do not replace the service. |
| aria2 exit code is unreadable (`$null`) | Report the artifact and the missing exit code; do not report a zero-length code as a non-zero exit. |
| async task fails | Report the artifact and task error after started tasks are joined; do not replace the service. |
| destination missing after success | Treat the download as failed and clean the staging directory. |
| local override missing | Fail with a source-specific error before configuration/service changes. |
| download failure with partial files | Release downloader resources and let the outer staging cleanup remove partial files. |

### 5. Good / Base / Bad Cases

- Good: two remote artifacts start concurrently, aria2 uses split connections,
  both complete, then `xbctl config init` and service installation run.
- Base: one artifact is supplied locally and the other downloads through the
  available path; the local artifact is copied without a network request.
- Bad: invoke `Invoke-WebRequest` sequentially for both remote artifacts, or
  install aria2 implicitly through winget/Chocolatey during the installer.

### 6. Tests Required

- Parse `install.ps1` on Windows PowerShell 5.1 without executing it.
- With a test `aria2c.exe`, assert both processes are started before either is
  waited on and that the expected split/server arguments and output names are
  passed.
- With a test `aria2c.exe` that exits 0, assert the download step reports
  success instead of reading a `$null` exit code as a failure; with one that
  exits non-zero, assert the reported message contains the real code.
- On ARM64 or with a forced bootstrap failure, use a controlled endpoint to
  assert both `DownloadFileTaskAsync` tasks start before either is awaited.
- Exercise one local override plus one remote artifact, a failed remote
  artifact, and missing output; assert service replacement is not reached and
  the staging directory is removed.
- Run the existing Windows lifecycle smoke test after a successful install.

### 7. Wrong vs Correct

#### Wrong

```powershell
Download-OrCopy $Binary $stagedNode $nodeArtifact
Download-OrCopy $XbctlBinary $stagedCli $cliArtifact
```

This makes the second release download wait for the first and provides no
per-file connection splitting.

```powershell
$process = Start-Process -FilePath $aria2Path -WorkingDirectory $stage `
    -ArgumentList $arguments -PassThru -NoNewWindow
$process.WaitForExit()
if ($process.ExitCode -ne 0) { throw "aria2c exited with code $($process.ExitCode)" }
```

`Start-Process -PassThru` hands back a `Process` object that the cmdlet did not
spawn, so `ExitCode` is `$null`. `$null -ne 0` is true, so the check fires after
a download that actually succeeded and the install aborts with the empty
message `aria2c exited with code `. `-ArgumentList` additionally joins the
array with spaces and quotes nothing.

#### Correct

```powershell
$startInfo = New-Object System.Diagnostics.ProcessStartInfo
$startInfo.FileName = $aria2Path
$startInfo.WorkingDirectory = $stage
$startInfo.UseShellExecute = $false
$startInfo.Arguments = ($arguments | ForEach-Object { ConvertTo-ProcessArgument $_ }) -join ' '
$process = New-Object System.Diagnostics.Process
$process.StartInfo = $startInfo
[void]$process.Start()
# repeat for the second artifact before waiting on either, then join both
$process.WaitForExit()
$exitCode = $process.ExitCode
if ($null -eq $exitCode) { throw 'aria2c exit code could not be determined' }
if ($exitCode -ne 0) { throw "aria2c exited with code $exitCode" }
```

A `Process` that started the child itself always has an `ExitCode` after
`WaitForExit()`.

When system aria2 and the verified bootstrap are unavailable, the equivalent
correct boundary is two `DownloadFileTaskAsync` calls followed by joining both
tasks.

## 9. Temporary aria2 bootstrap contract

### 1. Scope / Trigger

This contract applies when the installer wants aria2's segmented downloads but
`aria2c.exe` is not already available on the host.

### 2. Signatures

- `Get-Aria2Executable($Arch, $Stage)` returns an existing PATH executable, a
  verified temporary executable, or `$null` for the built-in downloader.
- The only bootstrap asset is the fixed official URL for
  `aria2-1.37.0-win-64bit-build1.zip`.
- The pinned SHA-256 is
  `67d015301eef0b612191212d564c5bb0a14b5b9c4796b76454276a4d28d9b288`.

### 3. Contracts

- A PATH `aria2c.exe` always takes precedence and skips bootstrap.
- Bootstrap is allowed only for `amd64`; `arm64` uses the built-in .NET path
  when no system aria2 exists.
- The ZIP is downloaded below the current unique staging directory, hashed
  before extraction/use, and must contain exactly one regular `aria2c.exe`.
- The temporary executable is never copied to Program Files, added to PATH,
  registered in the registry, or written into a service definition.
- The outer staging `finally` removes the archive and extracted executable after
  downloader processes have been joined or terminated.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| PATH aria2 exists | Use it; do not download bootstrap. |
| Architecture is ARM64 and PATH aria2 is absent | Skip x64 bootstrap and use .NET fallback. |
| ZIP download/hash/extraction fails | Warn, never execute the unverified file, and use .NET fallback. |
| Hash differs from the pinned digest | Treat bootstrap as unavailable and clean temporary files. |
| ZIP has zero or multiple `aria2c.exe` files | Treat bootstrap as unavailable and use .NET fallback. |
| Install or download fails after bootstrap | Terminate aria2 processes, then remove bootstrap resources with staging cleanup. |

### 5. Good / Base / Bad Cases

- Good: amd64 without PATH aria2 downloads the pinned ZIP, verifies it, runs the
  extracted `aria2c.exe`, and removes it after the install.
- Base: ARM64 or a network failure uses the existing concurrent .NET downloader
  without executing an x64 binary.
- Bad: execute `aria2c.exe` before hashing the ZIP, follow a moving `latest`
  bootstrap URL, or copy the executable into a permanent system directory.

### 6. Tests Required

- Assert PATH aria2 bypasses bootstrap.
- On amd64, assert the fixed URL and digest are used, a valid archive produces
  one executable, and bootstrap files are removed after success/failure.
- Assert hash mismatch and missing executable never start aria2 and select .NET
  fallback.
- Assert ARM64 never downloads the x64 bootstrap.
- Run PowerShell 5.1 parser/runtime and Windows lifecycle smoke tests on a
  Windows host.

### 7. Wrong vs Correct

#### Wrong

```powershell
Invoke-WebRequest $movingLatestUrl -OutFile $archive
Expand-Archive $archive $extract
& (Join-Path $extract 'aria2c.exe')
```

This executes a moving, unverified third-party binary.

#### Correct

```powershell
$hash = (Get-FileHash $archive -Algorithm SHA256).Hash
if ($hash -ine $aria2BootstrapSha256) { throw 'digest mismatch' }
Expand-Archive $archive -DestinationPath $extract
```

Only the verified temporary executable may be passed to
`ProcessStartInfo.FileName`.
