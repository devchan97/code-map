// Package walker enumerates indexable files honoring ignore rules and computing SHA-1 + language hints.
package walker

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/devchan97/code-map/internal/platform"
)

// Entry holds metadata for a single file that passed all ignore and secret
// filters and is ready for indexing.
type Entry struct {
	// Path is the repo-relative path, normalized to forward slashes.
	Path string
	// SHA1 is the lowercase hex-encoded SHA-1 digest of the file content.
	SHA1 string
	// Size is the file size in bytes.
	Size int64
	// Lang is the detected programming language based on file extension.
	// Empty string means the language is unknown.
	Lang string
}

// Options configures Walk behaviour.
type Options struct {
	// ExtraIgnores holds additional gitignore-style patterns applied on top of
	// .gitignore and .codemapignore.
	ExtraIgnores []string
	// FollowSymlinks makes Walk dereference symbolic links. A visited set
	// keyed by resolved absolute path guards against cycles.
	FollowSymlinks bool
	// MaxFileBytes is the maximum file size that will be read and indexed.
	// Files larger than this limit are skipped. Zero means the default of
	// 1 MiB (1<<20 bytes).
	MaxFileBytes int64
}

const defaultMaxFileBytes int64 = 1 << 20 // 1 MiB

// Walk enumerates files under root, applying ignore rules and yielding each
// kept file via the yield callback. Entries are delivered in deterministic
// (lexicographic) order because filepath.WalkDir already walks in sorted
// order. If yield returns an error, Walk aborts immediately and returns that
// error. Walk also honors ctx cancellation between entries.
func Walk(ctx context.Context, root string, opts Options, yield func(Entry) error) error {
	// 1. Resolve root to an absolute path.
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("walker.Walk: abs root: %w", err)
	}

	// 2. Determine effective file size limit.
	maxBytes := opts.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxFileBytes
	}

	// 3. Load ignore rules.
	ignoreSet, err := LoadIgnore(absRoot, opts.ExtraIgnores)
	if err != nil {
		return fmt.Errorf("walker.Walk: load ignores: %w", err)
	}

	// visited tracks resolved absolute paths for symlink cycle detection.
	var visited map[string]struct{}
	if opts.FollowSymlinks {
		visited = make(map[string]struct{})
	}

	// 4. Walk the file tree.
	return filepath.WalkDir(absRoot, func(absPath string, d fs.DirEntry, werr error) error {
		if werr != nil {
			slog.Debug("walker: skipping entry with error", "path", absPath, "err", werr)
			return nil
		}

		// Check context cancellation between entries.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Resolve symlinks when requested.
		if d.Type()&fs.ModeSymlink != 0 {
			if !opts.FollowSymlinks {
				// Treat the symlink as an opaque file: compute SHA-1 of the
				// link target string rather than following it.
				return handleSymlinkAsFile(ctx, absRoot, absPath, ignoreSet, maxBytes, yield)
			}
			resolved, err := filepath.EvalSymlinks(absPath)
			if err != nil {
				slog.Debug("walker: cannot resolve symlink", "path", absPath, "err", err)
				return nil
			}
			if _, seen := visited[resolved]; seen {
				slog.Debug("walker: skipping symlink cycle", "path", absPath, "resolved", resolved)
				return nil
			}
			visited[resolved] = struct{}{}
			// Re-stat the resolved path to get its real type.
			info, err := os.Stat(resolved)
			if err != nil {
				slog.Debug("walker: stat resolved symlink failed", "path", resolved, "err", err)
				return nil
			}
			if info.IsDir() {
				// Walk into the resolved directory by replacing the current path.
				return filepath.WalkDir(resolved, func(ap string, dd fs.DirEntry, we error) error {
					if we != nil {
						return nil
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
					return processEntry(ctx, absRoot, ap, dd, ignoreSet, maxBytes, yield)
				})
			}
			absPath = resolved
			d = fs.FileInfoToDirEntry(info)
		}

		return processEntry(ctx, absRoot, absPath, d, ignoreSet, maxBytes, yield)
	})
}

// processEntry handles a single DirEntry produced by WalkDir.
func processEntry(
	ctx context.Context,
	absRoot, absPath string,
	d fs.DirEntry,
	ignoreSet *IgnoreSet,
	maxBytes int64,
	yield func(Entry) error,
) error {
	// Compute the repo-relative path (forward-slash normalized).
	rel, err := platform.RelFromRoot(absRoot, absPath)
	if err != nil {
		slog.Debug("walker: cannot compute relative path", "abs", absPath, "err", err)
		return nil
	}

	if d.IsDir() {
		// Skip default excluded directory names (basename match).
		base := filepath.Base(absPath)
		for _, excl := range DefaultExcludeDirs {
			if strings.EqualFold(base, excl) {
				slog.Debug("walker: skipping excluded dir", "path", rel)
				return filepath.SkipDir
			}
		}
		// Skip dirs matched by ignore rules.
		if rel != "." && ignoreSet.Match(rel, true) {
			slog.Debug("walker: ignoring dir", "path", rel)
			return filepath.SkipDir
		}
		return nil
	}

	if !d.Type().IsRegular() {
		// Skip non-regular files (devices, pipes, …).
		return nil
	}

	// Skip files matched by ignore rules.
	if ignoreSet.Match(rel, false) {
		slog.Debug("walker: ignoring file", "path", rel)
		return nil
	}

	// Skip secret filenames before reading content.
	if IsSecretFile(rel) {
		slog.Debug("walker: skipping secret file", "path", rel)
		return nil
	}

	// Skip files that exceed the size limit.
	info, err := d.Info()
	if err != nil {
		slog.Debug("walker: cannot stat file", "path", rel, "err", err)
		return nil
	}
	if info.Size() > maxBytes {
		slog.Debug("walker: skipping oversized file", "path", rel, "size", info.Size())
		return nil
	}

	// Read the file and perform secret content scan.
	content, err := os.ReadFile(absPath)
	if err != nil {
		slog.Debug("walker: cannot read file", "path", rel, "err", err)
		return nil
	}
	if ContainsSecretPattern(content) {
		slog.Debug("walker: skipping file with secret pattern", "path", rel)
		return nil
	}

	// Compute SHA-1.
	sum := sha1.Sum(content)
	sha1Hex := fmt.Sprintf("%x", sum)

	entry := Entry{
		Path: rel,
		SHA1: sha1Hex,
		Size: info.Size(),
		Lang: detectLang(rel),
	}

	return yield(entry)
}

// handleSymlinkAsFile handles a symlink when FollowSymlinks is false.
// It computes SHA-1 over the link target string and yields it as a regular
// file entry (size = size of the link target bytes).
func handleSymlinkAsFile(
	_ context.Context,
	absRoot, absPath string,
	ignoreSet *IgnoreSet,
	maxBytes int64,
	yield func(Entry) error,
) error {
	rel, err := platform.RelFromRoot(absRoot, absPath)
	if err != nil {
		return nil
	}
	if ignoreSet.Match(rel, false) {
		return nil
	}
	if IsSecretFile(rel) {
		return nil
	}
	target, err := os.Readlink(absPath)
	if err != nil {
		slog.Debug("walker: cannot read symlink target", "path", rel, "err", err)
		return nil
	}
	content := []byte(target)
	if int64(len(content)) > maxBytes {
		return nil
	}
	if ContainsSecretPattern(content) {
		return nil
	}
	sum := sha1.Sum(content)
	return yield(Entry{
		Path: rel,
		SHA1: fmt.Sprintf("%x", sum),
		Size: int64(len(content)),
		Lang: detectLang(rel),
	})
}

// detectLang returns a canonical language identifier inferred from the file
// extension. Returns an empty string for unrecognized extensions.
func detectLang(relPath string) string {
	ext := strings.ToLower(filepath.Ext(relPath))
	switch ext {
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "ts"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "ts"
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}
