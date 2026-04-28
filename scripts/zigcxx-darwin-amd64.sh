#!/usr/bin/env bash
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE_DIR:-${RUNNER_TEMP:-/tmp}/zig-cache-darwin-amd64}"
exec zig c++ -target x86_64-macos "$@"
