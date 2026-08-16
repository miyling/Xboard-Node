# Windows Server support — implementation plan

## Ordered checklist

1. **Capture the implementation context**
   - Read the backend specs and the completed research notes.
   - Confirm the current worktree only contains the known Trellis/bootstrap
     changes before touching product files.
   - Start the task only after the final planning summary is explicitly
     approved.

2. **Make shared file/config behavior portable**
   - Add the credentials file loader and tests; add an optional absolute
     `-credentials` flag and load it before config env-ref resolution.
   - Add the platform replace-file helper with unique same-directory temp files
     and route certificate, GeoData, YAML, credentials, and metadata writes
     through it.
   - Fix the config-base fallback, watcher event/retry behavior, and Windows
     disk root with focused tests.
   - Make logger file-writer replacement close the previous owned handle.

3. **Refactor the node host lifecycle**
   - Make the main application loop context-driven and return errors instead of
     calling `os.Exit` from deep lifecycle paths.
   - Add Windows Service and console host adapters with build tags.
   - Add service status transitions, graceful stop/shutdown handling, and
     compile-time coverage for both host variants.

4. **Split `xbctl` by platform contracts**
   - Introduce platform path defaults and elevated-privilege checks.
   - Replace direct systemd calls in status, bind, remove, upgrade, and
     uninstall with the service contract.
   - Implement Unix adapters preserving current behavior and Windows SCM/file
     log adapters.
   - Add `service install/uninstall`, `--log-output`, Windows artifact naming,
     and safe Windows self-update/uninstall helpers.

5. **Add native Windows distribution**
   - Add Windows amd64 Make targets and clean rules.
   - Add `install.ps1` with install/upgrade/status/uninstall actions, rollback of
     staged files on failed health checks, ACL hardening for credentials, and
     node/machine mode arguments.
   - Add a Windows smoke-check script or CI job for build, service lifecycle,
     config loading, credentials, and `/healthz`; include a path-with-spaces
     install and a controlled-stop versus crash-recovery check.

6. **Update docs and examples**
   - Document Windows prerequisites, PowerShell commands, default paths, logs,
     ports/firewall, service commands, manual console mode, upgrade, and
     uninstall.
   - Document release artifact names and preserve Linux instructions.

7. **Quality gate and integration review**
   - Run formatting, unit tests, race tests, vet/static checks, Linux builds,
     Windows cross-builds, and PowerShell syntax checks where available.
   - Search for remaining unguarded Linux-only paths/commands in product code.
   - Review config/credential round trips and service failure/rollback behavior.

## Validation commands

Run locally when tools are available; otherwise run the equivalent commands in
the repository's Go Docker image or CI:

```bash
gofmt -w <changed-go-files>
go test -race -count=1 ./internal/...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/xboard-node ./cmd/xbctl
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/xboard-node ./cmd/xbctl
make build-windows
```

Windows-runner checks should include:

```powershell
.\xboard-node.exe -v
.\xbctl.exe version
.\xbctl.exe service install
.\xbctl.exe service start
Invoke-WebRequest http://127.0.0.1:65530/healthz
.\xbctl.exe status
.\xbctl.exe service stop
.\xbctl.exe service uninstall
```

## Risky files and rollback points

- `cmd/xboard-node/main.go`: lifecycle refactor; keep the pre-refactor
  application loop recoverable until console and service tests pass.
- `cmd/xbctl/main.go` plus platform adapters: command behavior and upgrade
  rollback; validate Linux adapter behavior before switching all call sites.
- `internal/cert/cert.go` and `internal/kernel/geodata/geodata.go`: file
  replacement semantics; retain existing temp-file cleanup on every error.
- `install.ps1`: installation can change service/filesystem state; stage under a
  unique temp directory and restore the prior binary/config/service state if
  health validation fails.
- `Makefile`/README/CI: release artifact names are externally consumed; search
  all references after changing them.

## Completion gates before task activation

- `prd.md`, `design.md`, and this file agree on full native Windows scope.
- Research notes are present and referenced by the manifests.
- `implement.jsonl` and `check.jsonl` contain real spec/research entries (not
  the seed example row).
- No blocking product decision remains; the final planning summary has been
  shown to the user and explicitly approved in a later message.
