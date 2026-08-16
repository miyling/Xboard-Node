# Windows Server support — technical design

## Architecture and boundaries

The node runtime remains one Go application. Windows support is added at the
host and distribution boundaries; panel contracts, node services, and embedded
sing-box/xray configuration remain shared.

### 1. Application host lifecycle

- Extract the current application loop from `cmd/xboard-node/main.go` into a
  context-aware function that loads the config, starts the watcher/services,
  and returns on cancellation or a fatal startup error.
- Add build-tagged host adapters for console mode and Windows Service mode.
  The Windows adapter uses the Windows Service Control Manager handler from
  `golang.org/x/sys/windows/svc`; console execution continues to use normal
  Ctrl+C/termination handling.
- The service handler reports `StartPending`, `Running`, `StopPending`, and
  `Stopped`, accepts stop/shutdown requests, and cancels the shared application
  context so node services drain normally.

### 2. Platform paths and service operations

- Keep the existing Linux paths and systemd behavior in a Unix build-tagged
  implementation.
- Add a Windows path implementation with these defaults:
  - install/config root: `%ProgramData%\\xboard-node`
  - binaries: `%ProgramFiles%\\Xboard Node\\bin\\xboard-node.exe` and
    `%ProgramFiles%\\Xboard Node\\bin\\xbctl.exe`
  - config: `<install root>\\config.yml`
  - credentials: `<install root>\\credentials.env`
  - metadata: `<install root>\\install-meta.json`
  - log: `<install root>\\logs\\xboard-node.log`
  - service name: `xboard-node`
- Move lifecycle operations behind a small platform service contract used by
  `xbctl`: status, install, start, stop, restart, enable, disable, uninstall,
  and logs. Linux delegates to systemd/journalctl; Windows uses the SCM APIs
  for service state/control and a file tailer for application logs.
- Replace the Unix-only `os.Geteuid` check with a build-tagged privilege check:
  Unix checks the effective UID; Windows checks whether the current process
  token is elevated.

### 3. Configuration, credentials, and files

- Add a small dotenv-compatible loader for the generated `credentials.env`.
  `xboard-node` accepts an optional absolute `-credentials` path and otherwise
  loads the sibling credentials file before resolving `token_env`, without
  overwriting explicitly supplied process environment variables. This
  preserves systemd's existing environment-file behavior and makes a Windows
  service independent of systemd. Parsing supports UTF-8 BOM, CRLF, comments,
  values containing `=`, and rejects malformed key/value lines without logging
  secret values.
- Keep config-relative defaults for data directories and change only the
  impossible-path fallback from `/etc/xboard-node` to the current working
  directory, so `filepath` remains the single path composition mechanism.
- Add a shared replace-file helper. It writes a unique same-directory temp
  file, flushes/closes it, and then uses `os.Rename` on Unix or the native
  replace-capable move operation on Windows. Certificate persistence, GeoData
  downloads, generated YAML, credentials, and install metadata use this helper
  so an existing destination is handled correctly on Windows and readers never
  observe a partial file.
- Expand the config watcher to recognize `Write`, `Create`, `Rename`, and
  `Remove` events and retry a short time after an atomic-save rename. The
  parent-directory watch remains the cross-platform source of truth.

### 4. Windows resource reporting

- Keep gopsutil for CPU, memory, swap, and network metrics.
- Add `monitor.CollectAt(dataRoot)` and pass each instance's config/data root
  from the service and machine layers. Resolve the disk query root by platform:
  `/` on Unix and the volume containing that absolute data path on Windows.
  Unsupported load-average data remains best-effort and is reported as zero
  when the library cannot provide it.

### 5. `xbctl` and installation

- Preserve existing command names and config format. Make path defaults,
  service operations, artifact naming, privilege checks, upgrade, and
  uninstall platform-aware instead of duplicating the whole CLI.
- Add `service install` and `service uninstall` operations so both installers
  can use the same service contract. Windows service installation points SCM at
  `xboard-node.exe -c <config> -credentials <credentials>` and configures automatic start plus restart
  recovery; it does not put secrets in the service command line.
- Add a `--log-output` option to `xbctl config init`. The Windows installer
  uses the file default above; manually managed installations can still select
  stdout/stderr.
- Make logger writer replacement close the previous owned file handle during a
  config reload, while leaving stdout/stderr unowned. Service/file output never
  enables ANSI colors and remains a plain append stream; rotation is deferred.
- Add `install.ps1` as the native Windows installer. It requires elevation,
  supports node and machine modes, downloads or accepts local amd64 binaries,
  preserves existing config on upgrade, writes credentials/meta, installs or
  updates the service, starts it, and waits for `/healthz`. Uninstall supports
  preserving config or purging the install root.
- Make Windows self-upgrade/uninstall safe for the running `xbctl.exe` by
  scheduling a detached PowerShell cleanup/replacement helper after the current
  process exits. The PowerShell installer remains the recommended full upgrade
  path because it can replace both binaries without a self-replacement hop.

### 6. Build and documentation

- Add Make targets for Windows amd64 artifacts with `.exe` suffixes and include
  them in the all-platform build. Keep Linux target names unchanged.
- Add Windows build/compile validation to CI or an equivalent reproducible
  Docker/Go command, plus a PowerShell smoke-check script for service and
  health operations when a Windows runner is available.
- Document PowerShell installation, manual config/service commands, default
  paths, log location, firewall/ACME port requirements, upgrade, uninstall,
  and console debugging. Keep the Linux section intact.

## Data flow and contracts

```text
PowerShell / xbctl
  -> platform paths + config init
  -> config.yml + credentials.env + install-meta.json
  -> Windows SCM starts xboard-node.exe -c config.yml -credentials credentials.env
  -> dotenv loader + YAML loader + normalized instances
  -> shared node/machine services and embedded kernels
  -> health endpoint + file log + panel status
```

The stable external contracts are:

- configuration YAML keys remain compatible with the existing format;
- `panel`, `machine`, `nodes`, certificates, and kernel settings retain their
  current semantics;
- `xbctl` command names remain compatible, with `service install/uninstall`
  added;
- release artifact names are `xboard-node-windows-amd64.exe` and
  `xbctl-windows-amd64.exe` (and optional arm64 variants when built).

## Compatibility and migration

- Existing Linux installs continue to use `/etc/xboard-node`, systemd,
  `EnvironmentFile`, and journalctl.
- Existing Windows manual configs can set absolute Windows paths; relative
  paths continue to resolve from the config file directory.
- A Windows install generated by the new PowerShell script can be upgraded in
  place without changing the YAML schema. Existing credentials are preserved
  unless a binding is explicitly replaced.
- No panel database or API migration is required.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Windows service stop arrives while a node is accepting connections | Route SCM stop through the shared context and retain the existing graceful drain timeout. |
| Atomic rename fails because another process has the file open | Use native replace semantics, retry transient sharing violations, and log a clear path-specific error. |
| Generated credentials are unavailable to SCM-launched processes | Pass the absolute credentials path to the service and load it before config resolution; never rely on an interactive shell. |
| `xbctl.exe` cannot replace itself while running | Use the detached post-exit helper and cover the generated command in tests/static checks. |
| Editors save config through rename/remove sequences | Watch the parent directory, handle rename/remove, and debounce/retry reload. |
| Windows resource metrics differ from Linux | Use platform disk roots and treat unsupported metrics as best-effort; verify on a Windows runner. |
| No Windows host is available locally | Require Windows cross-compilation and a Windows CI/manual smoke validation gate before release. |

## Deferred items

- Windows GUI installer, MSI packaging, code signing, and registry-based config
  are out of scope for this iteration.
- Windows firewall rule creation is documented, not silently changed by the
  installer.
- Service log rotation is deferred; the application log remains a single file
  and operators can use the host's rotation policy.
