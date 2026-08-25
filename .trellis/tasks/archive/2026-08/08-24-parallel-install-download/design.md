# install.ps1 parallel download — technical design

## Architecture and boundaries

Keep `install.ps1` as the only product file in scope. The change belongs at the
staging boundary inside `Install-OrUpgrade`: resolve both artifact descriptors,
download/copy them into the existing stage, then enter the unchanged
configuration, backup, replacement, service, and health-check sequence.

The download layer has three paths:

1. Local source path: validate and copy the caller-provided `-Binary` or
   `-XbctlBinary` into staging.
2. aria2 path: when `aria2c.exe` is discoverable on `PATH`, start one aria2
   process for every remote artifact. Each process uses the staging directory as
   its working directory, the staged destination filename (`xboard-node.exe` or
   `xbctl.exe`) as `--out`, and eight split connections with eight connections
   per server. Start all processes before waiting for any of them.
3. Built-in fallback: when aria2 is unavailable, create one .NET `WebClient`
   per remote artifact and call `DownloadFileTaskAsync` for every artifact
   before awaiting any task. This keeps Windows PowerShell 5.1 usable on a
   clean Windows Server host.

## Data flow and contracts

```text
Binary/XbctlBinary or Release URL
        -> artifact descriptors (source, URL, destination, filename)
        -> local Copy-Item OR aria2 process OR WebClient task
        -> staged xboard-node.exe + xbctl.exe
        -> existing Prepare-Configuration and install/rollback flow
```

- `Resolve-Artifact` and `Resolve-DownloadUrl` remain the source of truth for
  artifact names and release URLs.
- A remote download is considered successful only after its process/task has
  completed successfully and its destination is a regular file.
- Any remote failure aborts before `Prepare-Configuration`, so service state and
  the installed binaries are untouched by a failed download.
- The existing outer `finally` owns staging-directory removal. The new layer
  additionally cancels unfinished WebClient tasks, terminates unfinished aria2
  processes, disposes process/client objects, and surfaces a useful failure.
- No credentials, tokens, service arguments, install paths, or public script
  parameters are passed to aria2.

## Error and cleanup behavior

Start all remote jobs first. Wait for every started job so a failure does not
leave another writer operating against the staging directory. Collect the
first failure (and the artifact name) and throw after the started jobs have been
joined. In a `finally` block, cancel/terminate anything still active and dispose
all handles. The caller's existing staging cleanup then removes partial files.

aria2's non-zero exit code is treated as a download error. The fallback wraps
the task exception with the artifact name. Existing local-copy validation and
all post-download rollback behavior remain unchanged.

## Compatibility and trade-offs

- aria2 provides per-file segmented connections and works as an optional speed
  enhancement, but its proxy/TLS behavior is environment-dependent. The
  built-in .NET path is the no-dependency fallback.
- The implementation deliberately does not install aria2 through winget,
  Chocolatey, Scoop, or another package manager. A bootstrap download would
  make the installer less reliable and expand the trust boundary.
- Parallelism is limited to the two independent release assets. HTTP Range
  sharding is delegated to aria2 and is out of scope for hosts without aria2.
- The script remains compatible with Windows PowerShell 5.1; no PowerShell 7
  parallel pipeline feature is required.

## Validation and rollback

Validate syntax on a host with PowerShell, inspect the generated aria2 command
arguments, and exercise both branches with local test endpoints or a Windows
Server smoke host. Verify that a failed download never reaches service
replacement and that the existing `finally` removes the stage. Run
`git diff --check`; existing Go tests are a regression check because no Go
runtime code changes.
