# codemap

> [한국어 문서 / Korean docs →](./README-ko.md)

Open-source CLI that indexes repositories into embedded per-repo SQLite stores
and serves precise `file:line` lookups to coding agents (Claude Code, Codex).
**No LLM** — codemap is a pure retrieval layer; the calling agent supplies all
reasoning.

![codemap visualize — interactive symbol graph with search, focus mode, and dark mode](.github/assets/preview.gif)

```
agent  →  "where is the rate-limit logic?"
codemap →  src/api/middleware.py:142-178   func apply_rate_limit
            (4 callers, indexed 2026-04-28 18:12 KST)
agent  →  reads 36 lines instead of 1,800
```

## Why

In agentic coding, token cost is dominated by whole-file reads. The agent does
not know where the relevant code lives, so it pulls 500–2,000-line files to
find the 30 lines that matter. codemap removes that overhead by exposing a
small lookup primitive that returns identifiers + `file:line` ranges, so the
agent can partial-read instead of loading entire files.

## Highlights

- **Embedded per-repo SQLite store** at `<repo>/.codemap/index.db` plus a
  global registry at `~/.codemap/registry.toml`. No server, no daemon.
- **Lexical retrieval (BM25)** by default — zero ML dependency, fast on
  identifier-shaped queries (the dominant workload).
- **Optional encoder rerank** behind a build tag (`-tags encoder`) for
  prose-heavy queries. Off by default to keep the binary small and cold-start
  fast.
- **Tree-sitter parsers** for Python, Java, JavaScript, TypeScript, TSX,
  C#, and C++. Extract symbols (functions, methods, classes, variables,
  imports, constants) plus call / reference / inherit / import edges. Go and
  Rust adapters are scaffolded.
- **SKILL.md auto-install** via `codemap install-skill` so coding agents
  (Claude Code; Codex when its skill spec is final) call codemap before
  reading whole files.
- **Single static binary**, cross-compiled with zig-cc and distributed via
  Homebrew, GitHub Releases, and `go install`.
- **Local only.** Default builds make zero outbound network calls; the index
  never leaves your machine.

## Status

M1 – M6 complete. Python + 6 additional language parsers (Java, JavaScript,
TypeScript, TSX, C#, C++), `visualize` with `lastIndexed` surfaced
everywhere, SKILL.md installer, encoder rerank gated behind the `encoder`
build tag (ONNX wiring is a one-file swap), and a zig-cc + GoReleaser
release pipeline targeting darwin amd64 / arm64, linux amd64 / arm64, and
windows amd64.

## Build

Requires **Go 1.25+** and a **C toolchain** (CGO is enabled by tree-sitter).

```sh
go build ./cmd/codemap
```

For optional encoder rerank support (placeholder in v1, ONNX in M5):

```sh
go build -tags encoder ./cmd/codemap
```

A `Makefile` is provided:

```sh
make build   # builds ./bin/codemap
make vet     # go vet ./...
make ci      # vet + build + encoder-tag build
```

## Quick start

```sh
# 1. Initialise the index for a repo (idempotent — re-running is cheap).
codemap init /path/to/myrepo

# 2. Refresh the index incrementally (only changed files re-parsed).
codemap index .

# 3. List all known indexes on this machine.
codemap list

# 4. Search.
codemap search "applyRateLimit" --json

# 5. Look up a single symbol by qualname or by file:line.
codemap show api.middleware.apply_rate_limit
codemap show src/api/middleware.py:150

# 6. Graph queries.
codemap refs  api.middleware.apply_rate_limit   # incoming references / callers
codemap calls api.middleware.apply_rate_limit   # outgoing calls / references

# 7. Render an interactive graph.
codemap visualize . --open

# 8. Install the SKILL.md so coding agents pick up the workflow.
codemap install-skill --agent claude-code --scope user
```

All commands accept `--json` for agent consumption. The JSON schema is part of
the public contract — see `architecture.md` §6.2.

## CLI reference

| Command | Purpose |
| --- | --- |
| `codemap init [PATH]` | Idempotent setup. Creates `.codemap/`, registers in the global registry, runs the first incremental index. |
| `codemap index [PATH]` | Incremental index. Reuses the existing DB; only files whose SHA-1 changed are re-parsed. |
| `codemap reindex [PATH]` | Drop & rebuild `symbols` / `edges` / `tokens`. Use after a schema bump. |
| `codemap list [--json]` | Show every registered repo (NAME, PATH, LAST_INDEXED, FILES, SYMBOLS). |
| `codemap status [PATH\|NAME]` | Stats + `lastIndexed`. |
| `codemap forget [PATH\|NAME]` | Remove a registry entry (does not delete `.codemap/`). |
| `codemap search <QUERY> [flags]` | Primary lookup. Flags: `--repo`, `--top`, `--kind`, `--scope`, `--file`, `--rerank`, `--json`. |
| `codemap show <QUALNAME\|FILE:LINE>` | Definition + snippet. |
| `codemap refs <QUALNAME>` | Incoming references / callers. |
| `codemap calls <QUALNAME>` | Outgoing calls / references. |
| `codemap visualize [PATH\|NAME] [flags]` | Render `graph.html`. Flags: `--out`, `--open`. |
| `codemap install-skill` | Install SKILL.md. Flags: `--agent`, `--scope`, `--print`. |
| `codemap uninstall-skill` | Remove the installed SKILL.md. |
| `codemap version` | Print version, schema version, and active embedder. |

## Architecture

```
┌──────────────────────────┐
│ Coding Agent (Claude /   │
│ Codex) — all reasoning   │
└──────┬───────────────────┘
       │ argv + JSON
       ▼
┌──────────────────────────┐
│ codemap CLI              │
│  cmd/codemap → cli       │
│  pipeline / search /     │
│  graph / visualize ...   │
└──┬───────────────────────┘
   │
   ▼
walker → parser (tree-sitter) → lexical (BM25) → store (SQLite)
                                 └─ encoder rerank (optional, M5)
```

- The CLI process is short-lived; every invocation re-opens the SQLite file.
  Cold start budget: ≤ 50 ms.
- Per-repo index lives in `<repo>/.codemap/index.db` (gitignored).
- Global registry at `~/.codemap/registry.toml` answers "which DB does
  `codemap search` use?" via a 6-step resolver (see `architecture.md` §3.7).
- Parser, encoder, and indexer are decoupled: language adapters emit
  `core.Symbol` / `core.Edge` only; persistence is the store layer's
  responsibility.

For the full design rationale and decision summary see
[`codemap-design.md`](./codemap-design.md). For module-level architecture see
[`architecture.md`](./architecture.md).

## SKILL.md integration

`codemap install-skill` writes a SKILL.md that instructs Claude Code (or Codex
once the spec is final) to call codemap before reading whole files. The skill
body lives in [`skill/SKILL.md.tmpl`](./skill/SKILL.md.tmpl) and is rendered
with the binary name and version at install time.

Default install paths:

- Claude Code, user scope: `~/.claude/skills/codemap/SKILL.md`
- Claude Code, project scope: `<repo>/.claude/skills/codemap/SKILL.md`
- Codex: spec pending (verify at release time)

## Repository layout

```
cmd/codemap/                 # CLI entrypoint (package main)
internal/
  cli/                       # cobra subcommand handlers
  core/                      # domain types + sentinel errors
  walker/                    # filesystem enumeration + ignore rules + secrets
  parser/                    # tree-sitter adapter registry
    python/                  # Python adapter (CGO)
    java/                    # Java adapter (CGO)
    ts/                      # JavaScript / TypeScript / TSX adapters (CGO)
    csharp/                  # C# adapter (CGO)
    cpp/                     # C++ adapter (CGO)
    fallback/                # whole-file fallback for unknown languages
    {golang,rust}/           # scaffolded placeholders
  lexical/                   # tokenization + BM25 + TokenStore interface
  encoder/                   # optional dense rerank (build tag: encoder)
  store/                     # SQLite gateway (modernc.org/sqlite, pure Go)
  registry/                  # ~/.codemap/registry.toml + 6-step resolver
  pipeline/                  # init / index / reindex orchestration
  search/                    # search.Run + Show
  graph/                     # refs + calls
  visualize/                 # graph.html renderer
  skill/                     # SKILL.md installer
  platform/                  # OS abstraction (paths, atomic write, browser)
skill/SKILL.md.tmpl          # canonical SKILL.md template
scripts/                     # zig-cc wrapper scripts (one per release target)
.github/workflows/           # ci.yml, release.yml
.goreleaser.yml              # cross-platform release config (zig-cc + CGO)
```

## Privacy

- **Local only by default.** The CLI makes no outbound network calls. The
  index, registry, and any rendered HTML stay on your machine.
- **Secrets-aware walker.** Files matching common secret patterns
  (`.env`, `*.pem`, `*.key`, `id_rsa*`, `id_ed25519*`, ...) are skipped before
  reading. Content scanning rejects files containing AWS access-key prefixes
  or PEM headers.
- **No telemetry.** codemap does not phone home.

## Development

```sh
go test ./...        # all tests (CGO-free packages); python adapter tests run on CGO-enabled CI
go vet  ./...
go build ./...
go build -tags encoder ./...
```

CI (`.github/workflows/ci.yml`) runs on Linux / macOS / Windows. Tree-sitter
requires CGO; the Python parser tests are gated behind `//go:build cgo`. CI
runners have C toolchains pre-installed; on a CGO-less developer environment
the rest of the codebase still builds and tests cleanly.

Releases are produced by `.github/workflows/release.yml`, which runs
GoReleaser on a single `ubuntu-latest` runner and uses zig-cc as a universal
cross-compiler for every CGO target (darwin amd64 / arm64, linux amd64 /
arm64, windows amd64). One-line wrapper scripts in `scripts/` bake each
target's `-target <triple>` flag into `$CC` / `$CXX` so CGO sees a single
executable path — without that wrapper indirection, spaces in `CC=zig cc
-target …` break Go's link step. Triggered by every `v*` tag push. Windows
arm64 is deferred.

## License

[MIT](./LICENSE)
