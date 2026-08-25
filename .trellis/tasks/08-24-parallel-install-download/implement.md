# install.ps1 parallel download — implementation plan

## Ordered checklist

1. Read the task artifacts and Windows deployment contract; confirm the worktree
   contains unrelated Trellis/bootstrap changes that must remain untouched.
2. Add artifact-oriented download helpers in `install.ps1`:
   - discover optional `aria2c.exe`;
   - start and join one aria2 process per remote artifact with eight split/server
     connections;
   - provide the PowerShell 5.1/.NET asynchronous fallback;
   - preserve local source copying and clean up all process/task resources.
3. Replace the two sequential `Download-OrCopy` calls in `Install-OrUpgrade`
   with the new orchestration while preserving staging paths and all subsequent
   configuration/service/rollback code.
4. Review the diff for URL, filename, parameter, credential, and rollback
   regressions. Do not edit README or release metadata.
5. Run the quality checks below and address concrete failures only within scope.

## Validation commands

```bash
git diff --check
command -v pwsh || true
command -v powershell || true
```

On Windows PowerShell 5.1 or PowerShell 7, run a parser check without executing
the installer:

```powershell
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
  (Resolve-Path .\install.ps1), [ref]$null, [ref]$errors
) | Out-Null
if ($errors.Count -gt 0) { $errors | Format-List; exit 1 }
```

On a Windows test host, validate both `aria2c.exe` present and absent, including
one local override plus one remote download. Confirm both remote jobs start
before either is joined, failed downloads stop before service replacement, and
the stage is removed. The existing lifecycle smoke check remains the final
post-install validation.

## Risky files and rollback points

- `install.ps1`: process argument quoting, asynchronous task handling, and
  cleanup are the only risky areas. Keep the old staging and service rollback
  sequence intact.
- If aria2 invocation is incompatible with a host proxy/TLS setup, removing or
  renaming `aria2c.exe` must transparently select the built-in fallback.
- If the new download orchestration fails review, revert only the new helper
  block and the two call-site lines; do not revert unrelated working-tree
  changes.

## Completion gate

- `prd.md`, `design.md`, and this plan agree on aria2-first behavior with a
  built-in fallback.
- Both context manifests contain real spec/research entries.
- The implementation and quality-check sub-agents have reviewed the same
  Windows contract and research note.
