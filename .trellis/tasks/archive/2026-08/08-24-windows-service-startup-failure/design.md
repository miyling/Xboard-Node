# Windows service startup failure diagnostics — technical design

## Architecture and boundaries

Keep the application lifecycle shared between console and service hosts. The
Windows host adapter owns SCM status transitions and startup error reporting;
`xbctl` owns service-state polling and operator-facing error formatting; the
PowerShell installer remains an orchestration and rollback layer.

## Startup data flow

```text
SCM start
  -> host_windows.Execute: StartPending
  -> runApplicationWithReady
       -> credentials.env
       -> config.yml + normalized startup layout
       -> configured/fallback logger
       -> health listener + managed runtime launch
  -> ready: Running
  -> runtime error: diagnostic log + Stopped(non-zero)
```

The readiness boundary is intentionally local. It verifies that the process
has a valid configuration and has launched its managed runtime, but it does
not turn a panel/network dependency into a fake success. An initial fatal
runtime error after the boundary still produces `Stopped(non-zero)` and is
reported through the same diagnostic path.

## Application readiness contract

Add an internal optional readiness notification to the shared application
runner rather than duplicating config loading in the Windows adapter:

- Existing console callers continue to use `runApplication` unchanged.
- A Windows service caller uses a readiness-aware wrapper/callback/channel.
- Startup errors before readiness are sent once through the notification and
  returned normally for the host to record.
- The readiness notification is emitted after local config validation, logger
  setup, health-listener setup, and managed goroutine launch. Reload/runtime
  errors after that point are normal runtime failures, not a second startup
  notification.

The Windows handler waits on readiness, application completion, or SCM stop
requests while reporting `StartPending`. It reports `Running` only after
readiness, and reports `Stopped` with a zero code for controlled cancellation
or a non-zero code for an application error.

## Early diagnostic log

Implement a Windows-only host helper that appends a timestamped startup error
to `logs\xboard-node.log` under the config directory when the normal logger
has not yet been initialized. The helper:

- creates the parent directory and appends without truncating prior evidence;
- writes only the sanitized error and diagnostic context, never credential
  values or the credentials file contents;
- is best-effort and never replaces the original startup error;
- is also used for post-readiness fatal application errors observed by the SCM
  handler.

The configured logger remains the source of normal application logs. This
fallback exists only for the pre-logger failure window and keeps the path
consistent with the installer defaults.

## `xbctl` service polling contract

Extend the Windows wait loop to inspect each queried status:

- `Running` satisfies a start request and `Stopped` satisfies a stop request;
- if a start request observes `Stopped`, return immediately with the desired
  state, `Win32ExitCode`, `ServiceSpecificExitCode`, and the default log path;
- pending states continue polling with the current bounded timeout;
- Linux adapters and command names remain unchanged.

This turns the current symptom into an error such as “service stopped before
running (exit code …); inspect …” while the detailed application error lives
in the log.

## Installer behavior

On the existing install/upgrade catch path, print a bounded tail of the
configured startup log before cleanup/rollback when the file exists. Keep the
current service removal, wait, file restore, and previous-service restart
sequence. The diagnostics must not prevent rollback if the log is locked or
missing.

## Compatibility and rollback

- No change to the service name, absolute binary/config/credentials paths,
  automatic start type, or recovery actions.
- No token is added to `BinaryPathName`, command output, or diagnostics.
- Console mode continues to use Ctrl+C and the same shared runner.
- If the new service start path fails, the installer still removes only the
  failed new service and restores the pre-install snapshot.

## Test strategy

- Unit-test pure startup-log path/error-sanitization and Windows service-state
  error formatting with Windows build tags where needed.
- Add readiness/error-path coverage around the shared runner without requiring
  a live SCM.
- Cross-compile the Windows host and CLI packages; run existing Unix tests and
  static checks in CI.
- On Windows, run the real SCM lifecycle, force invalid config/credentials and
  failed machine discovery, assert immediate terminal-state reporting, inspect
  the log, and verify rollback plus normal controlled-stop behavior.
