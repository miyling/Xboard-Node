# Windows service startup failure diagnostics and recovery

## Goal

Make the native Windows installation path reliably distinguish a service that
started from one that exited during startup, and expose the actual startup
error to the operator. A machine-mode install such as the reported
`INSTANCE_FREE_ANDILIBA_CC_MACHINE_5_E27EE4_MACHINE_TOKEN` case must either
reach the running/healthy state or fail with an actionable error and a
diagnostic log, while the previous installation remains recoverable.

## Background and confirmed facts

- The reported installer output ends at `service did not reach running`; the
  installer then removes the newly installed service and reports only the
  PowerShell wrapper's exit code.
- `install.ps1` invokes `xbctl service start` before its HTTP health check. The
  current Windows service wait loop polls for 30 seconds and returns a generic
  message instead of reporting that the service reached `Stopped` and why.
- `cmd/xboard-node/host_windows.go` currently reports `Running` immediately
  after launching `runApplication` in a goroutine. Credential loading, config
  parsing, logger initialization, health listener setup, and machine node
  discovery can still fail after that status transition.
- Startup failures before `config.InitLogger` write only to stderr. A process
  launched by the Service Control Manager has no useful interactive stderr, so
  the failure is effectively hidden from the installer.
- The Windows installer already uses absolute paths and configures the default
  log path under `%ProgramData%\xboard-node\logs\xboard-node.log`; the
  diagnostics must preserve this layout and never put credentials in the SCM
  command line or logs.
- The previous Windows support task explicitly deferred real Windows service
  validation. The current development environment has no Go toolchain, so
  local Go tests and Windows builds cannot be claimed as passing.

## Requirements

### R1. Correct Windows service startup handshake

Keep the service in `StartPending` until the shared application has completed
its local startup boundary: credentials/config loading, logger setup, startup
layout validation, health listener setup, and launch of the managed runtime.
If that boundary fails, report `Stopped` with a non-zero exit code and retain
the original error for diagnostics. A normal SCM stop/shutdown remains a
controlled zero-exit stop.

### R2. Actionable `xbctl service start` errors

When `xbctl` waits for `Running`, it must detect a terminal `Stopped` state
immediately and return the Windows service exit codes together with the
configured log path. It must continue to handle pending states and successful
starts without changing Linux behavior.

### R3. Service-safe startup diagnostics

If startup fails before the configured logger is available, append a concise
timestamped error to the Windows fallback log beside the config file (the
installer's default path). Diagnostics must be readable by the installer and
foreground debugging, must not contain credential values, and must not require
an interactive user session.

### R4. Installer failure visibility and rollback

When install or upgrade fails during service start/health validation, show the
service error and the tail of the startup log when available, then preserve the
existing rollback behavior. A failed new installation must not leave a broken
service registered or overwrite the previous working installation.

### R5. Regression safety and verification

Keep foreground console mode, Linux systemd behavior, service recovery policy,
credential-file precedence, and existing Windows path contracts unchanged.
Add focused tests for the startup notification/error formatting and compile
coverage for the Windows host/service code. Document the Windows-runner checks
needed to validate the real SCM lifecycle.

## Acceptance Criteria

- [x] A Windows service that fails while loading credentials/config or during
      initial machine discovery transitions to `Stopped` with a non-zero exit
      code; `xbctl service start` reports that terminal state immediately with
      the exit code and log location instead of waiting for the generic timeout.
- [x] The fallback Windows log contains the actionable startup error when the
      normal configured logger could not be initialized, and tests prove that
      credential values are not written to the diagnostic message.
- [x] A successful Windows service start observes `StartPending` followed by
      `Running`; stop/shutdown still reaches `Stopped` with a controlled zero
      exit and does not trigger crash recovery.
- [x] `install.ps1` prints the service/log diagnostics on failure and still
      restores the prior binaries, config, credentials, metadata, and service
      state according to the existing rollback path.
- [x] Foreground console startup and Linux build/runtime paths remain
      compatible; existing repository tests continue to pass where the
      required toolchain is available.
- [ ] On a Windows runner, installation of the reported machine-mode shape is
      exercised with `service install/start/status/stop/uninstall`, the health
      endpoint, a forced startup failure, and rollback; the resulting log and
      exit-code output are captured as evidence.

## Verification notes

- Linux race tests, command-package tests, `go vet`, Windows amd64 compile-only
  tests/builds, and `git diff --check` pass in the local `golang:1.26` Docker
  image.
- Real Windows SCM, PowerShell parsing, and health/rollback execution remain
  pending because this development host is macOS without a Windows runner.

## Out of scope

- Changing panel APIs, machine authentication semantics, or the generated
  instance/token naming scheme.
- Automatically changing firewall rules, service accounts, ACL policy, or
  release packaging beyond what is needed to expose startup failures.
- Adding retries for a bad panel URL/token or masking a panel/API error as a
  successful service start. Those runtime causes should remain visible in the
  diagnostic log for a separate fix if the new evidence identifies one.

## Open questions

None blocking. The supplied output identifies the failure boundary but not the
underlying application error; the implementation must make that error
observable before claiming the installation issue is fully resolved.
