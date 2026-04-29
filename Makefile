.PHONY: build vet ci release-snapshot check-cgo

# CGO_ENABLED=1 is required: every tree-sitter language adapter pulls in C code.
# Without CGO, all parsers silently fall back to the whole-file adapter, producing
# 1 symbol per file and 0 edges. We force it here and verify a C compiler is reachable.
export CGO_ENABLED=1

check-cgo:
	@command -v $${CC:-gcc} >/dev/null 2>&1 || { \
		echo "error: C compiler not found (CC=$${CC:-gcc}). CGO is required for tree-sitter parsers."; \
		echo "  macOS:    xcode-select --install"; \
		echo "  Linux:    apt install build-essential   (or distro equivalent)"; \
		echo "  Windows:  install msys2 mingw64 and add C:/msys64/mingw64/bin to PATH"; \
		exit 1; \
	}

build: check-cgo
	go build -o bin/codemap ./cmd/codemap

vet:
	go vet ./...

ci: vet build
	go build -tags encoder ./...

# release-snapshot — build all release artifacts locally without publishing.
#
# Prerequisites:
#   - goreleaser v2+   (brew install goreleaser  OR  go install github.com/goreleaser/goreleaser/v2@latest)
#   - zig 0.12+        (brew install zig  OR  https://ziglang.org/download/)
#
# Expected output: dist/ directory containing per-platform tarballs/zips + checksums.txt
#
# Verify Linux static linking after build:
#   file dist/codemap_linux_amd64_v1/codemap   → "statically linked"
#   ldd  dist/codemap_linux_amd64_v1/codemap   → "not a dynamic executable"
release-snapshot:
	chmod +x scripts/*.sh
	sudo install -m 0755 scripts/zigcc-darwin-amd64.sh   /usr/local/bin/zigcc-darwin-amd64
	sudo install -m 0755 scripts/zigcc-darwin-arm64.sh   /usr/local/bin/zigcc-darwin-arm64
	sudo install -m 0755 scripts/zigcc-linux-amd64.sh    /usr/local/bin/zigcc-linux-amd64
	sudo install -m 0755 scripts/zigcc-linux-arm64.sh    /usr/local/bin/zigcc-linux-arm64
	sudo install -m 0755 scripts/zigcc-windows-amd64.sh  /usr/local/bin/zigcc-windows-amd64
	sudo install -m 0755 scripts/zigcxx-darwin-amd64.sh  /usr/local/bin/zigcxx-darwin-amd64
	sudo install -m 0755 scripts/zigcxx-darwin-arm64.sh  /usr/local/bin/zigcxx-darwin-arm64
	sudo install -m 0755 scripts/zigcxx-linux-amd64.sh   /usr/local/bin/zigcxx-linux-amd64
	sudo install -m 0755 scripts/zigcxx-linux-arm64.sh   /usr/local/bin/zigcxx-linux-arm64
	sudo install -m 0755 scripts/zigcxx-windows-amd64.sh /usr/local/bin/zigcxx-windows-amd64
	goreleaser release --snapshot --clean --skip=publish
