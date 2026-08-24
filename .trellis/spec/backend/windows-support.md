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
