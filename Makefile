.PHONY: build vet ci release-snapshot

build:
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
	goreleaser release --snapshot --clean --skip=publish
