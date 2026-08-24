# Research: Windows service startup failure analysis

## Scope

Analyze the reported installation failure against the current repository and
the archived Windows Server support design. This is a repository-backed
diagnosis, not a claim that the exact panel/runtime cause is known from the
short installer output.

## Evidence

- `install.ps1:88-109` installs, enables, starts the service, then waits for
  `/healthz`. A start failure is thrown before the health check can explain the
  failure.
- `cmd/xbctl/service_windows.go:226-277` waits up to 30 seconds for `Running`
  but does not treat `Stopped` as a terminal failure or include service exit
  codes. This produces the reported generic `service did not reach running`.
- `cmd/xboard-node/host_windows.go:60-101` starts `runApplication` in a
  goroutine and sends `Running` immediately. The application can still fail
  while loading credentials/config or while machine startup performs its first
  panel request.
- `cmd/xboard-node/main.go:47-67` loads credentials and config before
  `config.InitLogger`; an early error is printed to stderr only.
- `internal/machine/machine.go:74-80` returns an error when the initial machine
  node discovery fails. In the reported machine-mode shape this is one
  plausible early-exit path, but the supplied output does not identify whether
  it happened.
- The installer writes the configured log to
  `%ProgramData%\xboard-node\logs\xboard-node.log`, so a fallback writer can
  use the config directory and preserve the existing Windows layout.
- The prior research at
  `.trellis/tasks/archive/2026-08/08-16-windows-server-support/research/windows-service-management.md`
  explicitly recorded that real Windows service/SCM validation was not run.

## Decision

Fix the observability and state-machine defects first: defer the `Running`
status until local application startup is ready, report terminal stopped state
and exit codes from `xbctl`, and persist early startup errors to the Windows
log without credential values. Do not infer or silently alter panel/network
behavior from the current symptom.

## Verification constraint

The current environment has no `go` executable, so Go tests and Windows
cross-compilation must be run in CI or on a Windows/Go validation host.
