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

Build the Linux deployment binary from Windows/Codex:

```powershell
$env:GOCACHE = (Resolve-Path '.').Path + '\.gocache'
$env:GOMODCACHE = (Resolve-Path '.').Path + '\.gomodcache'
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
go build -o bin\alphaess-passive-linux-amd64 ./cmd/alphaess-passive
```
