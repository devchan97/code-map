#!/usr/bin/env bash
exec zig c++ -target x86_64-windows-gnu "$@"
