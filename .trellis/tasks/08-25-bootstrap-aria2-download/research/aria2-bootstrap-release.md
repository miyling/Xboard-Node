# Temporary aria2 bootstrap release

## Source and asset

The official `aria2/aria2` GitHub release API reports the stable release tag
`release-1.37.0` and the Windows x64 asset:

```text
https://github.com/aria2/aria2/releases/download/release-1.37.0/aria2-1.37.0-win-64bit-build1.zip
```

The ZIP contains a nested directory with `aria2c.exe` and documentation. The
asset has no published GitHub API `digest` field, so the implementation pins the
SHA-256 recorded from the official asset during research:

```text
67d015301eef0b612191212d564c5bb0a14b5b9c4796b76454276a4d28d9b288
```

The release has no Windows ARM64 asset. The existing installer supports
`amd64` and `arm64`; only `amd64` may use this bootstrap. ARM64 must keep the
built-in .NET download path.

## PowerShell compatibility

- Windows PowerShell 5.1 provides `Invoke-WebRequest -UseBasicParsing`,
  `Get-FileHash -Algorithm SHA256`, and `Expand-Archive`.
- A unique subdirectory below the installer's existing staging directory avoids
  global temp-name collisions and is removed by the existing outer `finally`.
- The executable is never copied to Program Files, added to PATH, registered in
  the registry, or installed as a service.

## Security and fallback

Download the ZIP, hash it before extraction/use, require a discovered
`aria2c.exe`, and return its path only after all checks pass. Any bootstrap
failure should warn and return no path so the caller uses its existing
PowerShell/.NET concurrent downloader. Do not execute a partially downloaded or
hash-mismatched file.
