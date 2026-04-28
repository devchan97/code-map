#!/usr/bin/env bash
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-${RUNNER_TEMP:-/tmp}/zig-cache-windows-amd64}"
exec zig cc -target x86_64-windows-gnu "$@"
