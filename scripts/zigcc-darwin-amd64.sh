#!/usr/bin/env bash
# zig-cc wrapper for darwin/amd64. Used as $CC by goreleaser/cgo to avoid
# space-splitting issues when CGO invokes the compiler.
exec zig cc -target x86_64-macos "$@"
