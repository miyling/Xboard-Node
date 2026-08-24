# Windows service startup failure diagnostics — implementation plan

## Ordered checklist

1. **Load implementation context and protect the worktree**
   - Read the backend Windows contract, archived Windows service research, and
     this task's PRD/design.
   - Confirm the only pre-existing changes are the known Trellis/bootstrap
     files; do not revert or include them in product changes.
   - Start the task only after this planning summary is explicitly approved.

2. **Add readiness-aware shared lifecycle plumbing**
   - Introduce an optional one-shot readiness notification around the shared
     application runner without duplicating credential/config loading.
   - Emit readiness after local startup validation, logger setup, health
     listener setup, and managed runtime launch.
   - Preserve existing Unix/console callers and return all fatal errors.

3. **Fix Windows SCM status and diagnostics**
   - Keep `StartPending` while awaiting readiness and handle stop/shutdown
     requests during that window.
   - Report controlled stop as zero and fatal startup/runtime errors as
     non-zero, recording the error to the fallback log.
   - Add Windows-focused tests for the pure helpers and compile coverage for
     the host adapter.

4. **Make `xbctl` report terminal startup failures**
   - Detect `Stopped` while waiting for `Running` instead of polling until the
     generic timeout.
   - Include service exit codes and the configured log path in the returned
     error; preserve pending-state and Linux behavior.
   - Add focused tests for the status/error formatting where the SCM can be
     represented without a live service.

5. **Expose diagnostics from the PowerShell installer**
   - Print a bounded tail of the Windows startup log in the existing failure
     path, guarding missing/locked logs.
   - Keep the current rollback ordering and verify diagnostics cannot stop
     cleanup or leak credentials.

6. **Quality gate**
   - Run `gofmt` on changed Go files.
   - Run `go test -race -count=1 ./internal/...`, `go test ./cmd/...`, and
     `go vet ./...` where the Go toolchain is available.
   - Run `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test`/`go build` for the
     affected Windows packages and the repository's release-tag build.
   - Run PowerShell syntax checks and `git diff --check`.
   - On a Windows runner, execute install/start/status/health/stop/uninstall,
     a forced invalid-start case, and the reported machine-mode installation.

## Validation commands

```bash
gofmt -w cmd/xboard-node/main.go cmd/xboard-node/host_windows.go \
  cmd/xbctl/service_windows.go
go test -race -count=1 ./internal/...
go test ./cmd/...
go vet ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test ./cmd/xboard-node ./cmd/xbctl
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/xboard-node ./cmd/xbctl
git diff --check
```

Windows validation must also inspect:

```powershell
& $cli service install
& $cli service start
& $cli status
Invoke-WebRequest http://127.0.0.1:65530/healthz
& $cli service stop
& $cli service uninstall
Get-Content "$env:ProgramData\xboard-node\logs\xboard-node.log" -Tail 100
```

## Risky files and rollback points

- `cmd/xboard-node/main.go` and `host_windows.go`: lifecycle/status ordering;
  keep the existing console runner usable until both host variants compile.
- `cmd/xbctl/service_windows.go`: SCM status fields and pending-state polling;
  retain the old bounded timeout as the final guard.
- `install.ps1`: diagnostic output runs inside rollback; all log reads must be
  best-effort and never replace the existing restore path.
- If the Windows runner shows a panel/API failure rather than a lifecycle
  defect, retain the new diagnostics and record that runtime cause separately;
  do not broaden this task into panel protocol changes.

## Completion gates before activation

- `prd.md`, `design.md`, and this plan agree on the diagnostics/handshake
  scope and have no blocking product decision.
- `implement.jsonl` and `check.jsonl` contain real spec/research entries.
- The final planning summary has been presented and explicitly approved in a
  later user message.
