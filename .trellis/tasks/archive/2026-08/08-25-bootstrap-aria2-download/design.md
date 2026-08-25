# Temporary aria2 bootstrap — technical design

## Architecture and boundaries

Modify only `install.ps1` in the product code. Keep the current artifact
orchestration and add a resolver between PATH discovery and the existing aria2
process branch:

```text
PATH aria2c.exe
        ├─ found: use it directly
        └─ absent + amd64: download fixed aria2 ZIP -> hash -> extract -> use
              └─ failure / arm64: return no path -> existing .NET fallback
```

The bootstrap is a performance enhancement, not an installation prerequisite.
The current `Invoke-ArtifactDownloads` call site remains before
`Prepare-Configuration`; no service, credentials, or installed binary state is
touched until all release artifacts are ready.

## Bootstrap contract

Add fixed constants for aria2 version, official asset URL, and SHA-256. A helper
such as `Get-Aria2Executable([string]$Arch, [string]$Stage)` should:

1. Return the resolved PATH executable when one exists.
2. Return `$null` for ARM64 when no system aria2 is present.
3. For amd64, create `$Stage\aria2-bootstrap`, download the fixed ZIP there,
   calculate its SHA-256, and compare case-insensitively to the pinned digest.
4. Expand the verified ZIP below the same unique directory and recursively find
   exactly one regular `aria2c.exe`; return its absolute path.
5. Catch bootstrap-only errors, emit a warning without secrets, and return
   `$null` so the caller selects the existing .NET path.

The caller passes the already-resolved `$arch` into
`Invoke-ArtifactDownloads`. If the helper returns a path, existing aria2
process creation is unchanged; if it returns `$null`, existing WebClient task
creation is unchanged.

## Lifecycle and cleanup

The bootstrap directory lives under the per-install staging directory. The
existing outer `finally` removes the ZIP and extracted executable after all
aria2 processes have joined. The aria2 branch's existing `finally` terminates
any still-running process before disposing process handles. This covers normal
success, download failure, install failure, and Ctrl+C paths where PowerShell
executes `finally`.

Bootstrap cleanup errors are suppressed or warnings only; they must not replace
the original installation or download error. No cleanup targets are global or
unresolved.

## Compatibility and security trade-offs

- Pinning an official release and digest is more auditable than downloading a
  moving `latest` URL, but requires a deliberate future update when aria2 is
  upgraded.
- The official release supplies Windows x64 but not ARM64; falling back on
  ARM64 avoids executing an incompatible binary.
- `Expand-Archive`, `Get-FileHash`, and `Invoke-WebRequest -UseBasicParsing`
  match the installer's Windows PowerShell 5.1 target. No package manager,
  PowerShell 7 parallel feature, PATH mutation, or permanent install is added.

## Error behavior and validation

The bootstrap must never return an unverified executable. Hash mismatch,
missing archive, extraction failure, or missing `aria2c.exe` produces a warning
and selects .NET fallback. The existing aria2 process exit-code and destination
file checks remain authoritative after bootstrap.

Validation should cover PATH aria2, temporary amd64 bootstrap, ARM64 fallback,
hash mismatch, missing executable, cleanup, and the existing local override plus
remote artifact combinations. On the current macOS development host, Windows
PowerShell parser and runtime checks are deferred.
