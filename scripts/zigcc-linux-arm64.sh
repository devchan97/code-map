#!/usr/bin/env bash
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-${RUNNER_TEMP:-/tmp}/zig-cache-linux-arm64}"
exec zig cc -target aarch64-linux-musl "$@"
