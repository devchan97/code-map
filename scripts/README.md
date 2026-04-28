# scripts/

zig-cc wrapper scripts used by GoReleaser for CGO cross-compilation.

## Why these exist

GoReleaser sets `CC` and `CXX` per build target via the `overrides` block in
`.goreleaser.yml`. When the value contains spaces (e.g. `zig cc -target
x86_64-linux-musl`), CGO/Go's build harness invokes `$CC` as a single
executable path, not a shell command, so the `-target ...` portion is lost
and the linker tries to resolve libc against the host system. On Linux
runners this surfaces as `relocation target stderr not defined` and dozens of
similar libc symbol errors — the exact failure mode v0.1.0 hit.

The fix is a one-line wrapper script per target that bakes the `-target`
flag in. CGO sees a single executable path; the script forwards `"$@"` to
zig with the right triple.

## Files

| Target            | CC wrapper                  | CXX wrapper                  |
|-------------------|-----------------------------|------------------------------|
| darwin / amd64    | zigcc-darwin-amd64.sh       | zigcxx-darwin-amd64.sh       |
| darwin / arm64    | zigcc-darwin-arm64.sh       | zigcxx-darwin-arm64.sh       |
| linux / amd64     | zigcc-linux-amd64.sh        | zigcxx-linux-amd64.sh        |
| linux / arm64     | zigcc-linux-arm64.sh        | zigcxx-linux-arm64.sh        |
| windows / amd64   | zigcc-windows-amd64.sh      | zigcxx-windows-amd64.sh      |

## Permissions

These must be executable. The release workflow runs `chmod +x scripts/*.sh`
after checkout so git's filemode handling on Windows authors does not block
execution on the Linux runner.

## Local snapshot build

```sh
chmod +x scripts/*.sh
goreleaser release --snapshot --clean --skip=publish
```

Requires zig 0.12+ and goreleaser v2 on PATH.
