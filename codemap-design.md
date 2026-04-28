# codemap — Design Document (v0.3, FINAL)

> Open-source CLI that indexes one or more repositories into embedded
> per-repo SQLite stores and serves precise `file:line` lookups to coding
> agents (Claude Code, Codex). codemap contains no LLM. It is a **retrieval
> layer**: the calling agent supplies all reasoning. **Implementation
> language: Go.**

---

## 1. Core Insight

In agentic coding, token cost is dominated by **whole-file reads**. The
agent does not know where the relevant code lives, so it pulls 500–2000-line
files to find the 30 lines that matter. codemap removes that overhead by
exposing a small lookup primitive:

```
agent  →  "where is the rate-limit logic?"
codemap →  src/api/middleware.py:142-178   func apply_rate_limit
            (4 callers, indexed 2025-04-27 18:12 KST)
agent  →  reads 36 lines instead of 1,800
```

Implications that shape every decision below:

- **No LLM inside codemap.** Generative models served via Ollama (or
  similar) are too slow and uneven on code retrieval at working-repo scale,
  and they are unnecessary because the parent agent (Claude Code, Codex) is
  the smart component. codemap stays dumb on purpose.
- **Embeddings are optional.** Lexical retrieval (BM25 over tree-sitter-
  derived symbols, with CamelCase/snake_case subword tokenization) is
  competitive for code-name and identifier queries, which dominate the real
  workload.
- **Retrieval bar is "candidate list good enough for the agent to filter,"**
  not "first hit must always be correct." This relaxes design considerably.
- **Natural language interpretation is the agent's job.** codemap takes
  identifier-shaped queries and structural filters; the agent translates
  user prose (in any language) into those queries.

---

## 2. Requirements (recap)

| # | Requirement |
|---|---|
| R1 | Open source, minimize external deps |
| R2 | Embedded per-repo index — no managed DB, no server |
| R3 | Graph visualization included (graphify-style) |
| R4 | Pipeline: select repo/folder → enumerate scripts → extract symbols (incl. local + global vars) → sequential indexing → visualize |
| R5 | Surface `lastIndexed` timestamp prominently |
| R6 | CLI search returns identifier + `file:line` range — fast edits, fewer tokens |
| R7 | Distribute as a CLI |
| R8 | Auto-install as a SKILL.md for Claude Code / Codex |
| R9 | When the skill triggers, the agent uses the CLI |
| R10 | Reuse existing indexes; refresh based on stored `indexed_at` |
| R11 | Manage multiple codebases — list, select, switch |

---

## 3. Architecture

```
                  ┌────────────────────────────────┐
                  │  Coding Agent                  │
                  │  (Claude Code / Codex)         │
                  │  ── all reasoning lives here   │
                  │  ── translates NL → tokens     │
                  └──────┬───────────────┬─────────┘
                         │ skill invoke   │ partial-reads
                         ▼               │ at file:line
                  ┌────────────────┐     │
                  │ codemap CLI    │ ────┘
                  │ (pure retrieval)│
                  └─┬───┬───┬──────┘
       index pipeline │   │   │ list / search / show / refs / calls / visualize
                      │   │   ▼
      ┌───────────────┴───┴───────────────────┐
      │ Walker → Parser (tree-sitter)         │
      │   → Lexical index (BM25 + subword)    │
      │   → (opt) ONNX encoder vectors        │
      │   → SQLite single-file store (atomic) │
      └───────────────┬───────────────────────┘
                      │
       ┌──────────────┴──────────────┐
       ▼                             ▼
  ~/.codemap/                  <repo>/.codemap/
    registry.toml                index.db
    state.toml (optional)        meta.json
                                 graph.html
```

- Per-repo index lives in `<repo>/.codemap/` (gitignored).
- Global registry at `~/.codemap/registry.toml` lists all repos the user has
  ever indexed on this machine. This is the answer to "which DB does
  `codemap search` use?" — see §6.
- Nothing leaves the user's machine in default config.

---

## 4. Retrieval Strategy

### 4.1 L0 — Lexical (default, always on)

- **Identifier tokenization.** Split CamelCase and snake_case into
  subwords; index full tokens and subwords in parallel. `getUserById` →
  `getUserById`, `get`, `User`, `By`, `Id`, `user`, `id`.
- **BM25** over `name`, `qualname`, `docstring`, `snippet` fields with a
  light field-weighting scheme.
- **Structural filters** at query time: `kind` (function|class|method|
  variable|import|constant), `scope` (global|class|local|param), file glob.
- Pure code, zero ML dependency. Strong on the dominant query type:
  "find this name / this concept by recognizable terms."

### 4.2 L1 — Encoder re-rank (opt-in)

- A small **bi-encoder** model — e.g., BGE-small-en, jina-embeddings-v2-small.
  ~30–100 MB on disk, shipped as ONNX.
- For multilingual prose queries, use **BGE-M3** or
  **multilingual-e5-small** (~120–500 MB). Same interface.
- **Not a generative LLM.** Specialized encoder; query-time inference is
  <100 ms on CPU, batchable.
- Used only as a **re-ranker** over L0's top-50, narrowing to top-10.
- Activation: `--rerank` flag or `.codemap/config.toml` setting.

### 4.3 What is explicitly excluded

| Approach | Reason |
|---|---|
| Ollama / local generative LLM for embeddings | Slow, quality uneven on code; the parent agent already provides reasoning. |
| External embedding APIs (OpenAI / Voyage / etc.) | Defeats the token-saving premise; raises confidentiality concerns for proprietary code. |
| Pure RAG-style chunked text embedding | Loses structural info; tree-sitter symbol granularity is more useful for code. |
| Building an LLM into codemap | Out of scope. Reasoning belongs to the calling agent. |

### 4.4 Natural-language queries — handled by the agent, not by codemap

codemap's query language is **identifiers + filters**, not free-form prose.
A query like

> `What functions are included when main.py's init is executed?`

is **not** something codemap interprets directly. The intended flow is:

1. The user asks the agent (Claude Code) the question above in any language.
2. The agent reads SKILL.md (§9) which instructs it to extract
   identifier-shaped tokens from the user's prose: here, `main.py` and
   `init`.
3. The agent calls codemap with structured queries:
   ```
   codemap search "init" --file "*main.py" --json
   codemap show main.init
   codemap calls main.init --json     # outgoing calls — answers the question
   ```
4. The agent composes a natural-language answer in the user's language
   from those structured results.

This division of labor is intentional and is what keeps codemap small,
fast, and language-agnostic. **L0 lexical alone matches all hard tokens
in the user's prose** (`main.py`, `init` above) — the prose between them
is irrelevant to retrieval. For richer prose-only queries with no hard
tokens, opt-in to L1 encoder rerank.

---

## 5. CLI Implementation Language — **Go (locked)**

### 5.1 Comparison summary

| Criterion | Go | Rust | Python | Node/TS |
|---|---|---|---|---|
| Single static binary | ✅ | ✅ | ⚠️ shiv/PyInstaller | ⚠️ pkg/bun |
| Cold start | 10–50 ms | 5–30 ms | 100–300 ms | 50–150 ms |
| tree-sitter | CGO required | native | wheels | native |
| Embedded store | sqlite, chromem-go | sqlite, lancedb | sqlite, lancedb | sqlite, lancedb |
| ML ecosystem | n/a — not needed | n/a | advantage erased | n/a |
| Cross-platform release | GoReleaser + zig-cc | cargo-dist | runtime-dependent | bundler-dependent |
| Dev velocity | medium | low | high | high |
| Skill-install friction | very low | very low | depends on user env | medium |

### 5.2 Decision

**Go.** Locked. Reasons:

- Single static binary aligns with R7/R8: `brew install codemap` then
  `codemap install-skill` — done. No Python runtime, no venv, no npm.
- Fast cold start matters: every agent search invokes the binary fresh.
  Across dozens of calls per session, 30 ms vs. 200 ms compounds.
- Without ML obligations, Python's only meaningful advantage is gone.
- CGO for tree-sitter is handled at build time (GoReleaser + zig-cc); end
  users download one prebuilt static binary per platform.

---

## 6. Multi-Index Management (R10, R11)

### 6.1 Two-tier storage

- **Per-repo store** at `<repo>/.codemap/index.db`. Self-contained; can be
  deleted without affecting other repos.
- **Global registry** at `~/.codemap/registry.toml`. One row per indexed
  repo, with mirror metadata (path, last_indexed, counts) so `codemap list`
  is instant without opening every SQLite file.

`registry.toml` shape:

```toml
[[index]]
name         = "myproject"          # short slug, derived from repo dir name; user-overridable
path         = "/Users/me/work/myproject"
created_at   = "2025-04-25T10:00:00Z"
last_indexed = "2025-04-28T14:30:00Z"
file_count   = 312
symbol_count = 4823
embedder     = "lexical"
schema_ver   = 1
```

### 6.2 Discovery — which index does `codemap search` use?

Resolution order, first match wins:

1. `--repo <NAME|PATH>` flag explicit.
2. cwd is inside a registered `path` → use that registry entry.
3. cwd contains its own `.codemap/index.db` (registered or not) → use it
   and auto-register if missing.
4. `~/.codemap/state.toml` has `default_repo` set → use that.
5. Exactly one entry in the registry → use it (with stderr notice).
6. Otherwise: error with the list of known names and a hint to use
   `--repo`.

### 6.3 Lifecycle

- `codemap init [PATH]` — idempotent. If `.codemap/` exists: keep it,
  refresh registry entry, print `lastIndexed`. If not: create the
  directory, initialize an empty SQLite, register in
  `~/.codemap/registry.toml`, then run a first incremental index.
- `codemap index [PATH]` — incremental refresh. Reuses existing
  `index.db`. Compares each file's SHA-1 against `files.sha1`; only
  changed files are re-parsed. Updates `Meta.indexed_at` and the registry
  mirror.
- `codemap reindex [PATH]` — drops and rebuilds `symbols`, `edges`,
  `tokens` tables. `files` rows refreshed. Use after a schema bump or if
  the index is suspected corrupted.
- `codemap forget [PATH|NAME]` — removes the registry entry only; does
  not touch `.codemap/`. Use to hide a project from `list`.
- `codemap list` — prints registry as a table (NAME, PATH, LAST_INDEXED,
  FILES, SYMBOLS); `--json` for agents.

### 6.4 Why a registry instead of just walking-up to find `.codemap/`

Walking-up works only when the user is inside the repo. The registry
enables:

- `codemap list` from anywhere.
- `codemap search foo --repo myproject` from anywhere.
- Cross-repo search later (M5+, optional): `codemap search foo --repo a,b,c`.
- Visibility into stale indexes (`last_indexed` older than N days).

---

## 7. Storage and Reuse Logic

### 7.1 SQLite layout

Single file `<repo>/.codemap/index.db`. Tables:

```
meta(key TEXT PRIMARY KEY, value TEXT)
   keys: schema_ver | repo_root | indexed_at | embedder
         | symbol_count | file_count

files(path PK, sha1, indexed_at, language, size_bytes)

symbols(id PK, name, qualname, kind, scope, file, line_start, line_end,
        parent_qualname, docstring, snippet, vec BLOB NULLABLE)

edges(from_qualname, to_qualname, kind, resolved BOOL)
   kinds: call | reference | inherit | import

tokens(symbol_id, token, field, weight)        -- inverted index for BM25
   indexed by token for fast lookup
```

All writes happen inside a single transaction per `index` run. SQLite
gives atomic commits, free backup (file copy), and good portability —
exactly what we need given there is no server component.

### 7.2 Indexing flow (incremental)

```
codemap index <path>
  1. Resolve path → registry entry (auto-register if new).
  2. Open <path>/.codemap/index.db (create if missing).
  3. Walk filesystem, honoring .gitignore + .codemapignore.
     For each candidate file:
       a. compute sha1
       b. compare to files.sha1
       c. if equal → skip
       d. if differ or absent → enqueue for parsing
  4. Parse changed files via tree-sitter:
       symbols, edges, tokens emitted per file.
  5. Begin SQLite transaction:
       DELETE FROM symbols/edges/tokens WHERE file IN (changed)
       UPSERT files
       INSERT new symbols/edges/tokens
       UPDATE meta.indexed_at = now()
       UPDATE meta.symbol_count, file_count
     Commit.
  6. Update registry.toml mirror (last_indexed, counts).
  7. Print summary: N files indexed, M skipped unchanged,
     last_indexed = ISO timestamp.
```

### 7.3 Reuse semantics

- Re-running `codemap index` on an unchanged repo finishes in well under
  a second (only walks + SHA-1s files; no parsing, no DB writes).
- `codemap status` reads only `meta` and `files` summary — no DB
  rewriting — so agents can call it cheaply on every session start.
- The agent should compare `meta.indexed_at` against the mtime of files
  it is about to edit; if the file is newer, run `codemap index` first
  (this rule is in SKILL.md).

### 7.4 Schema versioning

`meta.schema_ver` is bumped when symbol/edge/token shape changes.
codemap refuses to read a higher schema than it knows; users get a
clear "schema X, supported up to Y; run `codemap reindex`" message.

---

## 8. Data Model

```
File          path        # repo-relative
              sha1
              indexed_at  # ISO 8601, used by lastIndexed
              language
              size_bytes

Symbol        id
              name        # 'compute_total'
              qualname    # 'app.billing.compute_total'
              kind        # function | method | class | variable | import | constant
              scope       # global | class | local | param
              file
              line_start
              line_end
              parent_qualname?
              docstring?
              snippet     # 6–10 line excerpt
              vec?        # only if encoder enabled

Edge          from_qualname
              to_qualname        # may be unresolved name
              kind               # call | reference | inherit | import
              resolved : bool
```

**Variable mapping (R4) rules.**
- Module-top `Assign` → `scope=global`
- Class-body `Assign` / `AnnAssign` → `scope=class`
- Function-body `Assign` → `scope=local`, `parent_qualname=<function>`
- Function parameters → `scope=param`
- `import` / `from x import y` → `kind=import`, `scope=global`

**`lastIndexed` is exposed in (R5):**
- `codemap status` and `codemap list` output
- `graph.html` header
- per-result in every `search` JSON record (`indexed_at` field)

---

## 9. Components

### 9.1 Walker
Honors `.gitignore` plus an optional `.codemapignore`. Default excludes:
`.git node_modules .venv venv __pycache__ dist build .codemap target .next
.mypy_cache .pytest_cache .ruff_cache`. Incremental via SHA-1.

### 9.2 Parser
tree-sitter as the unified backend, one adapter per language implementing
`parse(file) -> ([Symbol], [Edge])`. Initial set: Python, JavaScript/
TypeScript, Go, Rust. Files of unknown extension fall back to whole-file
chunking — still searchable via L0 over content.

Target language set for M3 (user's primary codebases): Java, JavaScript,
TypeScript, TSX, C#, C++. Go and Rust remain in the adapter registry for
completeness but are lower priority.

### 9.3 Indexer
For each changed file: parse → emit symbols and edges → tokenize for BM25 →
(optional) embed with the encoder → upsert into SQLite inside one
transaction. Bumps `Meta.indexed_at`.

### 9.4 Store
Single-file SQLite per repo (§7.1). One global registry TOML.

### 9.5 Search
1. Detect query shape (identifier-like vs. natural language).
2. BM25 over `tokens` → top-50 with structural filters.
3. If `--rerank` and encoder available: dense re-rank → top-10.
4. Emit JSON:
   `[{file, line_start, line_end, qualname, kind, scope, snippet, score, indexed_at}]`.

### 9.6 Graph queries — `refs` (incoming) and `calls` (outgoing)
Symmetric pair, both backed by the `edges` table.
- `codemap refs <qualname>` — every site that references the symbol.
- `codemap calls <qualname>` — every symbol the given function/method
  invokes or references in its body.
This pair is what makes the example query "what functions does
`main.init` call?" answerable in one round trip.

### 9.7 Visualizer
Single static `graph.html`. One CDN dep at view time (vis-network or
cytoscape.js). Header shows `lastIndexed`, repo path, node/edge counts.
Filters: kind, scope, file glob.

---

## 10. CLI Specification (final)

| Command | Purpose |
|---|---|
| `codemap init [PATH]` | Idempotent setup. Create `.codemap/`, register in `~/.codemap/registry.toml`, run first incremental index. If already initialized, just refreshes the registry mirror and prints `lastIndexed`. |
| `codemap list [--json]` | Show all registered indexes (NAME, PATH, LAST_INDEXED, FILES, SYMBOLS). |
| `codemap index [PATH]` | Incremental index. Reuses existing DB; only changed files re-parsed. |
| `codemap reindex [PATH]` | Drop + rebuild symbols/edges/tokens. |
| `codemap forget [PATH\|NAME]` | Remove registry entry (does not delete `.codemap/`). |
| `codemap status [PATH\|NAME]` | Stats + `lastIndexed`. |
| `codemap search <QUERY>` | Flags: `--repo NAME\|PATH`, `--top N`, `--kind`, `--scope`, `--file GLOB`, `--rerank`, `--json` |
| `codemap show <QUALNAME\|FILE:LINE>` | Definition + snippet. `--repo` supported. |
| `codemap refs <QUALNAME>` | Incoming references (callers). `--repo` supported. |
| `codemap calls <QUALNAME>` | Outgoing calls (callees). `--repo` supported. |
| `codemap visualize [PATH\|NAME]` | Flags: `--out`, `--open`. |
| `codemap install-skill` | Flags: `--scope user\|project`, `--agent claude-code\|codex`, `--print`. |
| `codemap uninstall-skill` | — |
| `codemap version` | Includes index format version + active embedder. |

All commands accept `--json`. Agents always pass `--json`.

Repo resolution for every query/show/refs/calls/visualize call follows §6.2.

---

## 11. SKILL.md Integration

### 11.1 Install location

| Agent | Scope | Path |
|---|---|---|
| Claude Code | user | `~/.claude/skills/codemap/SKILL.md` |
| Claude Code | project | `<repo>/.claude/skills/codemap/SKILL.md` |
| Codex | user | (verify against current Codex skill spec at release time) |

`codemap install-skill` writes the file. `--print` dumps the SKILL body
to stdout.

### 11.2 SKILL.md body (English — for reliable agent triggering)

```
---
name: codemap
description: Use whenever the user asks to find, navigate, refactor, or
  understand code in the current repository — e.g., "where is X used",
  "find the function that does Y", "what does function Z call", "what
  depends on W". ALWAYS try `codemap search` BEFORE reading whole files.
  Returns precise file:line ranges so you can partial-read instead of
  loading entire files. This is the primary way to keep context-token
  usage low on this codebase.
---

# When to use
- Any user question about location, definition, callers, or dependencies
  of a symbol — in any language (e.g., a Korean question about an English
  codebase).
- Before any refactor that touches a name appearing in multiple files.
- At session start, run `codemap status` to confirm the index exists and
  is fresh.

# Translating user prose to codemap queries
codemap takes identifier-shaped queries and structural filters. When the
user writes prose (any language), do this:
  1. Extract identifier-shaped tokens: file paths (e.g. `main.py`),
     function/class/variable names, module paths.
  2. Identify the question type:
       "where is X used"          → refs
       "what does X call"         → calls
       "where is X defined"       → search + show
       "find code about <topic>"  → search with topic keywords
  3. Issue the corresponding command.

Example. User: "What functions are included when main.py's init is executed?"
  Tokens: main.py, init
  Plan:
    codemap search "init" --file "*main.py" --json     # locate init
    codemap calls <qualname-of-init> --json            # answer
  Answer the user in their language using the returned list.

# Commands
- `codemap status --json`              check index existence and lastIndexed
- `codemap list --json`                see all known indexes if not in repo
- `codemap init .`                     first-time setup (idempotent)
- `codemap index .`                    refresh stale index
- `codemap search "<q>" --json`        primary lookup; default top 10
- `codemap show <qualname>`            fetch definition snippet
- `codemap refs <qualname> --json`     find callers
- `codemap calls <qualname> --json`    find callees
- `codemap visualize . --open`         only on explicit user request

# Output handling
- For each result, read only `file` between `line_start` and `line_end`.
- Do NOT read the full file unless the snippet is insufficient.
- If `indexed_at` is older than the file you are about to edit, run
  `codemap index .` first.
- `--rerank` only when the user explicitly asks for higher precision.
```

### 11.3 Why English

Claude Code and Codex parse SKILL.md to decide when and how to invoke a
skill. Triggering reliability is highest in English. The same applies to
this design document.

---

## 12. Distribution

| Channel | Status |
|---|---|
| GitHub Releases (darwin-arm64/amd64, linux-arm64/amd64, windows-amd64) | primary |
| Homebrew tap | yes |
| `go install github.com/.../codemap@latest` | yes |
| `curl … \| sh` installer | yes |

CI: GitHub Actions + GoReleaser. tree-sitter CGO handled by zig-cc cross
toolchain so all release artifacts are fully static.

---

## 13. Repository Layout

```
codemap/
├── cmd/codemap/                 # main package, CLI entrypoint
├── internal/
│   ├── walker/
│   ├── parser/
│   │   ├── python/
│   │   ├── ts/
│   │   ├── golang/
│   │   └── rust/
│   ├── lexical/                 # BM25 + tokenization
│   ├── encoder/                 # optional ONNX rerank
│   ├── store/                   # SQLite per repo
│   ├── registry/                # ~/.codemap/registry.toml management
│   ├── search/
│   ├── visualize/
│   └── skill/                   # SKILL.md templating + install
├── skill/SKILL.md.tmpl
├── docs/design.md               # this document
├── .github/workflows/
└── README.md
```

User runtime layout:

```
~/.codemap/
├── registry.toml                # all known indexes
└── state.toml                   # optional: default_repo

<each-repo>/.codemap/
├── index.db                     # SQLite
├── meta.json                    # mirror of meta table for quick reads
├── config.toml                  # optional, e.g. enable rerank
└── graph.html                   # visualization
```

---

## 14. Roadmap

| Milestone | Deliverable | Status |
|---|---|---|
| M0 | Design freeze (this doc) | ✅ |
| M1 | Go skeleton: walker, Python tree-sitter parser, SQLite store, BM25 search, registry, `init/index/list/status/search/show/refs/calls` | ✅ |
| M2 | `visualize` + lastIndexed surfaced in CLI and HTML | ✅ |
| M3 | Multi-language parsers: Java, JavaScript, TypeScript, TSX, C#, C++ (Go/Rust deferred) | ✅ |
| M4 | `install-skill`, SKILL.md, golden + drift-guard tests | ✅ (end-to-end agent loop is a manual validation step) |
| M5 | Optional encoder rerank (build-tag split, cosine in `search`) | ✅ placeholder; ONNX session wiring is a one-file swap in `onnx_enabled.go`. Held-out set top-1 validation deferred until a real model is wired. |
| M6 | Cross-platform releases via zig-cc + GoReleaser; Homebrew tap stanza | ✅ pipeline ready. Tap repo creation + `HOMEBREW_TAP_TOKEN` are manual setup steps before the first tagged release. |

---

## 15. Open Questions (non-blocking)

1. tree-sitter via CGO vs. WASM (purego). CGO simpler at runtime; WASM
   avoids cross-build pain. Default to CGO for v1.
2. BM25 implementation: Bleve vs. hand-rolled FTS over SQLite.
   Hand-rolled is preferred for binary size and weighting control;
   confirm during M1.
3. Edge resolution depth for dynamic languages (Python duck typing,
   JS/TS structural).
4. Visualization library at scale: vis-network vs. cytoscape.js at 10k+
   nodes; may need clustering or progressive disclosure.
5. SKILL spec drift: confirm exact paths and frontmatter for both Claude
   Code and Codex at release time.
6. Secrets policy: auto-skip `.env`, `*.pem`, `*.key`, files matching
   common secret patterns; document overrides.
7. Monorepo: index per repo root, per workspace, or per declared
   subproject. Default = per repo root for v1.
8. Cross-repo search (`--repo a,b,c`) — defer to post-v1.

---

## 16. Decision Summary

- **Architecture role.** codemap is a retrieval layer. Reasoning stays
  with the calling agent.
- **No generative LLM** is invoked by codemap, locally or remotely.
- **Default retrieval.** BM25 over tree-sitter symbols with subword
  tokenization. Zero ML deps.
- **Optional re-rank.** Small ONNX encoder (BGE-small or multilingual
  variant). Not on by default.
- **Natural-language handling.** The agent extracts identifier tokens
  from prose and issues structured codemap commands; codemap itself does
  not interpret prose.
- **Multi-codebase support.** Per-repo `.codemap/index.db` plus a global
  `~/.codemap/registry.toml`. Resolution: `--repo` flag → cwd inside
  registered repo → cwd has `.codemap/` → default → unique entry → error.
- **Reuse semantics.** `codemap init` is idempotent and surfaces
  `lastIndexed`; `codemap index` is incremental via SHA-1; `reindex` is
  the explicit rebuild path.
- **Graph queries.** `refs` (incoming) + `calls` (outgoing) symmetric
  pair backed by the `edges` table.
- **Language.** Go. Locked.
- **Storage.** Embedded SQLite per repo; global TOML registry.
- **Distribution.** Prebuilt static binaries; `codemap install-skill`
  registers the skill.
- **Privacy.** Index never leaves the user's machine in default config.
- **Cost to user.** Zero — no managed DB, no API keys, no subscriptions.
