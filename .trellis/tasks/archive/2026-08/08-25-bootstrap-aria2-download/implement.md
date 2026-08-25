# Temporary aria2 bootstrap — implementation plan

## Ordered checklist

1. Read the task artifacts, previous Windows installer contract, and bootstrap
   research. Confirm unrelated Trellis/bootstrap files remain outside the
   product change.
2. Add fixed aria2 release constants and a helper that discovers PATH aria2 or,
   for amd64 only, downloads/verifies/extracts the temporary official ZIP.
3. Pass the existing architecture value into the download orchestrator and use
   the temporary path when bootstrap succeeds; preserve the current .NET
   fallback for ARM64 and all bootstrap failures.
4. Review lifecycle ordering, digest comparison, output paths, warnings, and
   staging cleanup. Do not change release asset URLs or service behavior.
5. Run available static checks and dispatch the quality-check agent.

## Validation commands

```bash
git diff --check
node -e "const fs=require('fs'); for (const p of ['.trellis/tasks/08-25-bootstrap-aria2-download/implement.jsonl','.trellis/tasks/08-25-bootstrap-aria2-download/check.jsonl']) for (const line of fs.readFileSync(p,'utf8').trim().split(/\\r?\\n/)) JSON.parse(line);"
command -v pwsh || true
command -v powershell || true
```

On Windows PowerShell:

```powershell
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
  (Resolve-Path .\install.ps1), [ref]$null, [ref]$errors
) | Out-Null
if ($errors.Count -gt 0) { $errors | Format-List; exit 1 }
```

Exercise PATH aria2, no PATH aria2 on amd64, ARM64 fallback, invalid digest,
missing archive executable, one local override plus one remote artifact, and
failed download cleanup. Verify the bootstrap directory is gone and the
installed service never contains the temporary aria2 path.

## Risky files and rollback points

- `install.ps1`: fixed URL/hash, PowerShell 5.1 archive APIs, and cleanup are
  the only new risk points.
- If bootstrap behavior is wrong, remove only the resolver and its call-site
  plumbing; the previous PATH aria2 and .NET branches must remain usable.
- Do not broaden the task to ARM64 third-party binaries or package-manager
  installation.

## Completion gate

- PRD, design, research, and this plan agree on amd64-only temporary bootstrap
  with hash verification and fallback.
- Both context manifests contain real spec/research entries.
- Final review records unavailable Windows-only checks explicitly.
