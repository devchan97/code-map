#!/usr/bin/env bash
# zig-cc wrapper for darwin/amd64. Used as $CC by goreleaser/cgo to avoid
# space-splitting issues when CGO invokes the compiler.
# Per-target ZIG_LOCAL_CACHE_DIR avoids "AccessDenied" errors when goreleaser
# builds multiple targets in parallel and zig races on its global cache.
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-${RUNNER_TEMP:-/tmp}/zig-cache-darwin-amd64}"
exec zig cc -target x86_64-macos "$@"
