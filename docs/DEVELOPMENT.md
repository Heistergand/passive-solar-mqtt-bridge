# Development

## Windows / Codex Sandbox

Go is expected to be available in `PATH`.

The Codex sandbox may not be allowed to write to the default Go build cache under the Windows user profile. Use a workspace-local cache when running Go commands:

```powershell
$env:GOCACHE = (Resolve-Path '.').Path + '\.gocache'
$env:GOMODCACHE = (Resolve-Path '.').Path + '\.gomodcache'
go test ./...
```

The `.gocache/` and `.gomodcache/` directories are ignored by Git.

Capture files, local build outputs, local Go caches, and MQTT secrets are ignored by Git. Do not add files from `pcaps/`, `bin/`, `.gocache/`, or `.gomodcache/`.

Avoid running Git from WSL against the Windows checkout under `/mnt/c` unless line-ending behavior is intentionally configured and verified. Use PowerShell Git for this repository to avoid a working tree full of line-ending-only changes.

Build the Linux deployment binary from Windows/Codex:

```powershell
$env:GOCACHE = (Resolve-Path '.').Path + '\.gocache'
$env:GOMODCACHE = (Resolve-Path '.').Path + '\.gomodcache'
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
go build -o bin\passive-solar-mqtt-linux-amd64 ./cmd/passive-solar-mqtt
```

## Releases

Release tags use plain semantic versions without a leading `v`:

```powershell
git tag -s 0.1.1 -m "0.1.1"
git push origin 0.1.1
```

Pushing such a tag starts the `Release` GitHub Actions workflow. The workflow runs tests, builds Linux `amd64` and `arm64` binaries, packages release tarballs, writes `checksums.txt`, and creates the GitHub release.

The first public release was `0.1.0`. Current `main` contains an installer password-file validation fix that should become `0.1.1`.

## Local Safety Checklist

Before committing:

```powershell
git status --short --ignored
git diff --check
go test ./...
```

Only source, docs, configs, scripts, and workflow files should be staged. Ignored local artifacts such as `bin/`, `pcaps/`, `.gocache/`, `.gomodcache/`, and `.tmp/` must stay out of commits.
