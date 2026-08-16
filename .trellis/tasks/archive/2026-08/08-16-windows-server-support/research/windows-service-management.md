# Research: Native Windows Server service management

- Query: Choose the Windows Server service-management approach for this Go repository and identify Windows-specific risks in paths, credential loading, logging, atomic replacement, config reload, upgrade/rollback, and disk metrics.
- Scope: mixed
- Date: 2026-08-16

## Findings

### Recommendation

Use the official `golang.org/x/sys/windows/svc` protocol directly in a Windows-only service adapter, and use the sibling `golang.org/x/sys/windows/svc/mgr` APIs for install, query, start, stop, delete, and recovery-action configuration. Keep the existing foreground execution path for console/debug mode. Use a PowerShell installer as an orchestration/documentation layer, but do not make `sc.exe` or `New-Service` subprocesses the runtime control plane.

This is the smallest native design that fits the repository: `x/sys` is already present indirectly at v0.42.0, while the application currently has no service abstraction and Linux lifecycle control remains in shell/systemd. A Windows build-tagged adapter can report `StartPending`, `Running`, `StopPending`, and `Stopped` through the SCM contract while reusing the existing node/machine execution and config reload logic.

The adapter must receive a cancellation context from the service stop control. The current `main` path only listens for `SIGINT`/`SIGTERM` (`cmd/xboard-node/main.go:98-132`), which is not sufficient for a Windows SCM stop request. It also calls `os.Exit` for runtime failures (`cmd/xboard-node/main.go:67-69`, `86-90`, `136-141`, `216-220`, `227-228`); service mode should return errors to the adapter so the SCM can observe a controlled stop or failure and apply recovery policy.

### Service-management alternatives

| Option | Evidence and API notes | Decision |
| --- | --- | --- |
| Direct `x/sys/windows/svc` plus `svc/mgr` | `svc.Handler.Execute(args, requests, status)` is the native callback contract. `svc.Run` dispatches the service, `svc.IsWindowsService` distinguishes service/interactive mode, and `mgr.Service` provides `Start`, `Control`, `Query`, `Delete`, `Config`, and `SetRecoveryActions`. Repository-compatible version is the already-selected `golang.org/x/sys v0.42.0`; promote it from indirect to direct when imported. | **Recommended.** Windows-only build tags keep Linux builds clean, status/control behavior is explicit, and no shell quoting or localized command parsing is required. |
| `github.com/kardianos/service` | The upstream Windows implementation wraps the same `x/sys/windows/svc`, `svc/mgr`, registry, and Event Log APIs. Its current `master` `go.mod` requires Go 1.23 and `x/sys v0.34.0`; it offers `Service.Run`, install/uninstall, `EnvVars`, `OnFailure`, delayed auto-start, and Event Log integration. | Rejected for this task. It adds a cross-platform abstraction the repository does not otherwise use, duplicates the direct adapter, and its registry-based environment feature could encourage putting panel/DNS secrets into service configuration. Reconsider only if Linux/macOS app-managed service installation becomes a product requirement. Pin a release rather than `master` if it is later adopted. |
| `sc.exe` / PowerShell `New-Service` wrapper | Microsoft documents `New-Service` as Windows-only and supports `-BinaryPathName`, `-StartupType`, `-Credential`, dependencies, description, and security descriptor. `CreateServiceW` requires a fully qualified binary path and requires quoting when the path contains spaces. `New-Service` does not itself provide the complete recovery-action policy; that requires SCM APIs or an additional `sc failure`/`ChangeServiceConfig2` step. | Rejected as the primary control plane. It still requires a real `svc.Handler` in the executable, and subprocesses add quoting, elevation, localized output, exit-code, and testability problems. It is acceptable as a documented fallback/bootstrap wrapper that invokes a native installer command. |

Suggested service contract:

1. `xboard-node.exe -c C:\ProgramData\xboard-node\config.yml -credentials C:\ProgramData\xboard-node\credentials.env` runs the same foreground application when launched interactively.
2. When launched by SCM, a Windows-only handler sends `StartPending`, starts the application under a context, then sends `Running` with `AcceptStop|AcceptShutdown`. On stop/shutdown it sends `StopPending`, cancels the context, waits for all node/machine goroutines and file handles to close, then returns.
3. `xbctl.exe service` calls `svc/mgr` directly for status/start/stop/restart/install/uninstall. Install sets an absolute quoted `BinaryPathName`, automatic or delayed-auto start, a description, and `ServiceRestart` recovery actions with a reset period. Keep `FailureActionsOnNonCrashFailures` false unless the product explicitly wants a nonzero controlled exit to restart.
4. Use `mgr.Query`/`QueryServiceStatusEx` and wait for the requested terminal state before replacing binaries or issuing a second start. Windows service commands are asynchronous from the caller's perspective.

Windows SCM has an important recovery nuance: Microsoft documents that a queued restart action cannot be canceled and may restart after a manual stop unless the service is explicitly disabled. Controlled upgrade/stop code must therefore verify the installed failure-action policy and test that a normal service stop does not create an unexpected restart.

### Repository paths and defaults

- `cmd/xboard-node/main.go:29` defaults to the relative `config.yml`. A Windows service's current directory is not a reliable application directory, so SCM must store an absolute config path in the service command line, or startup must resolve a deterministic install root before loading config.
- `internal/config/config.go:300-309` already derives the data base directory from the config file location, which is a good cross-platform pattern. Its error fallback is hard-coded to `/etc/xboard-node` (`:306`) and must not remain a Windows fallback.
- The Windows installer should separate immutable binaries from writable data. A practical default is a quoted binary path under `C:\Program Files\Xboard Node\bin` and writable config, credentials, certs, GeoData, metadata, backups, and logs under `C:\ProgramData\xboard-node`. The exact product path belongs in the plan/spec, but all defaults must be constructed with `filepath.Join` and passed as absolute paths.
- `internal/config/config.go:265-279`, `556-604`, and `775-809` already derive per-instance and per-node directories with `filepath.Join`; preserve that behavior and test paths containing spaces, drive letters, and (if supported) UNC paths. Do not use `/`, string concatenation, or an assumed service working directory.
- `cmd/xbctl/main.go:24-33` hard-codes Linux paths, the `.service` suffix, `/usr/local/bin`, and `/etc`. These constants need OS-specific implementations, not scattered `runtime.GOOS` conditionals. Windows defaults should include a stable service name without `/` or `\\`; Microsoft says service names are case-insensitive and slash/backslash are invalid.

### `credentials.env` loading and service accounts

The application currently has no credentials-file loader. Linux works because the generated unit injects the file with `EnvironmentFile` (`install.sh:599-609`), after which `internal/config/config.go:439-446` reads `os.Getenv`. The generated file is created and merged by `xbctl` (`cmd/xbctl/main.go:1450-1480`), but no Go runtime code reads it.

Windows service startup must explicitly load the credentials file before `config.LoadRoot` (`cmd/xboard-node/main.go:38`) and before `resolveEnvRefs` runs. Recommended behavior:

- add an explicit credentials path option (or a service-provided absolute default), load it before config parsing, and preserve already-set process environment precedence if that is the intended contract;
- parse the generated `KEY=value` format without shell evaluation; support UTF-8 BOM, CRLF, blank lines, comments, and values containing `=`; reject malformed keys/lines rather than silently loading a partial secret set;
- call `os.Setenv` only for validated keys, never log values, and make missing-file behavior explicit (optional file for backward compatibility versus required file when a `token_env`/`dns_env` reference is used);
- grant the service identity read access to the file and parent data directory, while keeping the file ACL restricted. `New-Service -Credential`/`CreateServiceW` credentials are the Windows service logon account, not the panel token; they do not replace this application-level loading contract;
- do not put panel tokens or DNS provider secrets into the SCM registry `Environment` value merely to avoid implementing file loading. It makes secret rotation/ACL auditing less clear and diverges from the existing credentials-file design.

The same loader should be used by console mode and Windows service mode so a manually started binary and SCM-started binary resolve `token_env` identically.

### Logging

`internal/config/config.go:813-857` defaults to stdout and otherwise opens an append-only file. `internal/nlog/nlog.go:38-55` stores the writer globally, and `InitLogger` can replace it during config reload. In a Windows service there is no interactive console to inspect stdout; choose a service default log file under the writable data root or add a build-tagged Event Log writer. If file logging is retained, close the prior `*os.File` when reconfiguring, synchronize writer replacement, and define rotation/retention (the current code never closes or rotates the old writer).

Do not rely on `journalctl` on Windows. `xbctl service logs` should either tail the configured file with a bounded/read-only implementation or query the Windows Event Log if an Event Log writer is selected. Ensure service startup errors before logger initialization still go to a useful file or Windows event source, not only an invisible service stdout handle. Disable ANSI coloring for service/file output; the current `term.IsTerminal` checks (`internal/config/config.go:829-849`) are a useful starting point.

### Atomic file replacement and Windows sharing rules

The repository has several replacement paths:

- certificate persistence writes a fixed `path + ".tmp"` and calls `os.Rename` (`internal/cert/cert.go:82-110`);
- GeoIP/GeoSite downloads use a fixed `dst + ".tmp"`, close it, then rename (`internal/kernel/geodata/geodata.go:77-105`);
- `xbctl config` writes credentials and metadata directly with `os.WriteFile` (`cmd/xbctl/main.go:1450-1526`);
- Linux upgrade downloads fixed `.new` files and renames them over installed binaries while the service is expected to run (`cmd/xbctl/main.go:404-463`).

For Windows, the plan must make replacement a shared helper with these guarantees: create a unique temporary file in the destination directory and therefore the same volume; write, flush, close, set intended ACL/mode, and then replace; clean up on every failure; retry only known transient sharing violations with a bounded delay; and never use one fixed temp name for concurrent writers. `fsnotify`'s Windows backend is based on `ReadDirectoryChangesW` and reports create/write/remove/rename operations, so replacement is observable but not necessarily as a single `Write` event.

Do not assume a running `.exe` can be atomically overwritten on Windows. Stop the service and wait for `Stopped` before replacing `xboard-node.exe`. A running `xbctl.exe` also cannot reliably replace itself; use a separate updater process/PowerShell helper or leave CLI replacement to the installer. For data files, close readers before replacement and test files opened by embedded kernels. If a destination is held open without delete sharing, `os.Rename`/MoveFileEx-style replacement fails; do not silently fall back to a partially written destination.

The config writer must also become atomic. Direct `os.WriteFile` for YAML/metadata can expose a partially written file to the watcher or process crash recovery. Preserve the existing parent-directory watch and make all config/meta/cert/GeoData replacements use the same tested helper. `ReplaceFileW`/`MoveFileExW(MOVEFILE_REPLACE_EXISTING)` are the relevant native primitives when the Go abstraction cannot replace an existing Windows target; both require same-volume/ACL/sharing behavior to be validated on the supported Server versions.

### `fsnotify` config reload

The current watcher makes the correct high-level choice of watching the parent directory (`internal/config/watcher.go:40-45`, `73-75`) so an atomic save can be observed. It then filters only `Write` and `Create` (`:112-123`). That is incomplete for Windows: fsnotify v1.9.0's Windows backend maps `ReadDirectoryChangesW` move notifications to `Rename` and destination moves to `Create` (`backend_windows.go` source, lines 208-219), and atomic-save tools may emit rename/remove/create sequences. The reload predicate should include `Rename` (and handle `Remove`/missing destination by retrying after debounce), filter with normalized absolute paths, and tolerate a short period where the destination is absent.

Keep the one-second debounce, but make reload robust against transient `os.IsNotExist`/sharing violations: retry reads for a bounded interval, then keep the prior config and log one actionable error. Watcher errors must be observable; fsnotify documents a Windows `ReadDirectoryChangesW` backend and an overflow error when its buffer is full. A Windows stress test should perform repeated atomic replacements, rename-away/recreate, malformed writes, and burst updates, asserting one stable reload and no lost watcher.

### Upgrade and rollback

The current Linux upgrade sequence backs up binaries, renames staged files, restarts systemd, and rolls back on restart failure (`cmd/xbctl/main.go:438-511`). It is not portable to Windows because it replaces the service executable while it may still be open and because the running `xbctl.exe` is itself a replacement target.

Recommended Windows transaction:

1. Download/stage service and CLI artifacts in a same-volume versioned staging directory. Verify release version, architecture, and (preferably) a published checksum/signature before touching the install.
2. Snapshot current binary paths, service configuration, config, credentials, and metadata into a versioned backup. Keep the backup until post-start health succeeds.
3. Stop through SCM and poll `QueryServiceStatusEx` until `Stopped`; do not replace files after merely issuing `ControlService`.
4. Replace the service binary and metadata using the shared Windows-safe replacement helper. Run the CLI updater from a separate path/process so it is not overwriting its own image. Keep the service's absolute `ImagePath` stable if possible; versioned directories plus a stable path simplify rollback.
5. Start the service, wait for `Running`, then poll `/healthz` and verify the reported version/instance state. Only then remove old backups.
6. On any replacement/start/health failure, stop the new service, restore the previous version/config/metadata, start it, and verify health. Report if rollback health fails and retain artifacts for diagnosis. Do not claim success merely because the SCM start call returned.

Test the interaction with SCM recovery actions: a controlled stop during upgrade must not be mistaken for a crash and must not be followed by a delayed unexpected restart. Also test interrupted upgrades, stale staging directories, reboot during staging, and a failed replacement due to an open handle.

### gopsutil disk metrics

`internal/monitor/monitor.go:153-156` calls `disk.Usage("/")`. The gopsutil v4.26.2 Windows implementation calls `GetDiskFreeSpaceExW` with the supplied path, and its public API documents `Usage` as taking a filesystem path. `/` is therefore not a valid portable choice for Windows Server. Pass a real path associated with the installation/config/data volume, derive its root with `filepath.VolumeName`, or inject a configured metrics path. For UNC paths, preserve the UNC root rather than blindly concatenating a drive letter.

The current best-effort treatment of `load.Avg()` (`:137-141`) is acceptable if Windows tests allow unsupported load averages to remain zero, but disk metrics must be asserted against the configured volume. Add a monitor constructor or setter that receives the resolved config/data root; avoid using `os.Getwd()` as the sole source because SCM working directories are not stable. Test a Windows path with spaces and a non-system drive, and verify `DiskTotal`, `DiskUsed`, and no error log from the metric collector.

### Affected repository files

- `cmd/xboard-node/main.go`: extract a context-returning foreground runner; add Windows service entry/status handling; load credentials before config; keep console mode.
- `cmd/xbctl/main.go`: split OS-specific defaults and service operations; replace Linux `sudo/systemctl/journalctl` paths (`:292-310`, `:375-378`, `:469-499`, `:543-555`) with native SCM operations; make config/meta/upgrade writes atomic; move self-update into a helper/installer flow.
- `internal/config/config.go`, `internal/config/watcher.go`: remove Linux fallback default, define absolute Windows data defaults, add credentials-file integration, and handle `Rename`/transient replacement events.
- `internal/config/config_test.go` and new Windows-tagged service/path/reload tests: cover path defaults, env loading, absolute service arguments, atomic saves, and Windows event sequences.
- `internal/nlog/nlog.go`: support service-safe file/Event Log output and close/swap writers safely.
- `internal/cert/cert.go`, `internal/kernel/geodata/geodata.go`: replace fixed-temp `os.Rename` helpers with the shared safe replacement contract and Windows tests.
- `internal/monitor/monitor.go`: inject a Windows-valid disk path/volume root; keep unsupported load-average behavior best effort.
- `Makefile`, `README.md`, `config.yml.example`, and a new PowerShell installer/upgrade script: publish `windows/amd64` (at minimum), absolute data layout, service account/ACL rules, logs, and rollback procedure while preserving Linux targets.
- `go.mod`/`go.sum`: make `golang.org/x/sys v0.42.0` a direct dependency if the native service adapter imports it. `fsnotify v1.9.0` and `gopsutil/v4 v4.26.2` are already direct and have Windows implementations.

### Related specs

The applicable project guidance is `.trellis/spec/backend/logging-guidelines.md`, `error-handling.md`, `quality-guidelines.md`, and `directory-structure.md`, plus `.trellis/spec/guides/cross-layer-thinking-guide.md`. The backend files are currently templates rather than repository-backed contracts, so they do not resolve Windows behavior; the implementation plan should explicitly define the service/config/installer boundaries and error behavior. The active requirements are in `.trellis/tasks/08-16-windows-server-support/prd.md`.

### Release-blocking validation items

- [ ] On a real Windows Server runner (at least supported amd64 Server versions), install into a path containing spaces, grant the selected service identity access to `ProgramData`, and exercise install, enable/automatic start after reboot, status, start, stop, restart, disable, uninstall, and foreground console mode.
- [ ] Confirm `xboard-node.exe` receives SCM stop/shutdown, reports pending/running/stopped states, exits cleanly within the service stop timeout, and does not rely on POSIX signals.
- [ ] Test crash recovery and controlled upgrade separately; verify failure actions do not restart after a normal stop and do restart after an actual crash according to policy.
- [ ] Test `credentials.env` with CRLF, UTF-8 BOM, comments, values containing `=`, missing file, malformed line, `token_env`, and DNS provider credentials. Verify no secret appears in logs, SCM configuration, or error text, and verify service-account ACLs.
- [ ] Test config/meta/cert/GeoData atomic replacement while the service is running, including open-reader/sharing-violation cases, interrupted writes, rename-away/recreate, and concurrent writers. Assert no partial YAML/JSON/PEM is ever consumed.
- [ ] Test fsnotify reload on Windows for `Write`, `Create`, `Rename`, `Remove` followed by `Create`, burst events, and buffer overflow/recovery; assert malformed/transient files retain the last valid configuration.
- [ ] Run an end-to-end upgrade with the service running and `xbctl` invoked from its installed location. Verify the updater does not try to overwrite its own executable, waits for `Stopped`, validates version and health, and restores the previous version on failure.
- [ ] Verify disk metrics use the configured Windows volume (including a non-`C:` path and a path with spaces) and that unsupported load-average data is handled as documented.
- [ ] In CI, compile/test `GOOS=windows GOARCH=amd64` (and the chosen additional architectures) with the repository's Go 1.26 toolchain, run Windows-tagged tests on Windows Server, and run the existing Linux test/installer checks to prevent systemd regressions. The PRD records that the current development machine lacks a Go toolchain, so these checks are currently release-blocking rather than locally verified.

## External references (primary)

- Go `x/sys/windows/svc` source (handler/status contract and `svc.Run`): https://github.com/golang/sys/blob/v0.42.0/windows/svc/service.go
- Go `x/sys/windows/svc/mgr` source (`CreateService`, `Query`, `Start`, `Control`, `Delete`, config): https://github.com/golang/sys/blob/v0.42.0/windows/svc/mgr/service.go and https://github.com/golang/sys/blob/v0.42.0/windows/svc/mgr/config.go
- Go `x/sys/windows/svc/mgr` recovery actions: https://github.com/golang/sys/blob/v0.42.0/windows/svc/mgr/recovery.go
- Microsoft Service Control Manager overview: https://learn.microsoft.com/en-us/windows/win32/services/service-control-manager
- Microsoft `CreateServiceW` (absolute/quoted binary path and service account details): https://learn.microsoft.com/en-us/windows/win32/api/winsvc/nf-winsvc-createservicew
- Microsoft `ChangeServiceConfig2W` (failure actions and queued restart nuance): https://learn.microsoft.com/en-us/windows/win32/api/winsvc/nf-winsvc-changeserviceconfig2w
- Microsoft PowerShell `New-Service`: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.management/new-service
- Microsoft `MoveFileExW`: https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw
- fsnotify v1.9.0 Windows backend and FAQ on parent-directory watches/atomic saves: https://github.com/fsnotify/fsnotify/blob/v1.9.0/backend_windows.go and https://github.com/fsnotify/fsnotify/blob/v1.9.0/README.md
- gopsutil v4.26.2 Windows disk implementation: https://github.com/shirou/gopsutil/blob/v4.26.2/disk/disk_windows.go and API: https://github.com/shirou/gopsutil/blob/v4.26.2/disk/disk.go
- kardianos/service Windows implementation (comparison only): https://github.com/kardianos/service/blob/master/service_windows.go

## Caveats / Not Found

- The configured web search MCP returned HTTP 404 in this session, so primary references were retrieved directly from the official Go, Microsoft Learn, fsnotify, gopsutil, and kardianos source URLs with `curl`. URLs and versions are recorded above.
- No Windows host or Go toolchain is available in the current environment; no Windows service, SCM, file-sharing, event-notification, or cross-compiled test was executed.
- The repository has no existing Windows service module, credentials loader, Windows installer, or Event Log integration. The recommendation therefore identifies required boundaries and validation rather than claiming implementation compatibility.
- `kardianos/service` was inspected at its upstream `master` source because no version is selected by this repository. Its `go.mod` currently declares Go 1.23 and `x/sys v0.34.0`; do not treat `master` as a release-pinned dependency.
