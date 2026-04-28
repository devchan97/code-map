#!/usr/bin/env bash
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-${RUNNER_TEMP:-/tmp}/zig-cache-darwin-arm64}"
exec zig cc -target aarch64-macos "$@"
