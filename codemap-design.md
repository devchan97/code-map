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
    state.toml (optional)        graph.html (after `visualize`)
    bin/codemap (after `install-self`)
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
- Activation: `--rerank` flag on `codemap search`. There is no
  per-repo config file in v0.1.x; CLI flags are the only knob.

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
- Cross-repo search later (post-v1; tracked in §15 Q8): `codemap search foo --repo a,b,c`.
- Visibility into stale indexes (`last_indexed` older than N days).

---

## 7. Storage and Reuse Logic

### 7.1 SQLite layout

Single file `<repo>/.codemap/index.db`. Tables:

```
meta(key TEXT PRIMARY KEY, value TEXT)
   keys: schema_ver | indexer_ver | repo_root | indexed_at | embedder
         | symbol_count | file_count

files(path PK, sha1, indexed_at, language, size_bytes)

symbols(id PK, name, qualname, kind, scope, file, line_start, line_end,
        parent_qualname, docstring, snippet, vec BLOB NULLABLE)

edges(from_qualname, to_qualname, kind, resolved BOOL, file)
   kinds: call | reference | inherit | import
   note:  the `file` column is what binds an edge to its source; it
          enables the per-file cascading delete used by incremental
          re-indexing.

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

### 7.4 Versioning: schema_ver vs indexer_ver

Two version numbers live in `meta`. Different meanings, different
policies on mismatch:

- **`schema_ver`** — on-disk SQLite layout. Bumped when the table
  shape, columns, or index structure change in a way that an older
  binary can no longer read correctly. Mismatch is a **hard error**
  caught at `Open()` time; the user sees `schema X, supported up to
  Y; run codemap reindex` and the command exits non-zero. No
  automatic migration in v1.
- **`indexer_ver`** — parser / tokenizer / edge-resolver semantics.
  Bumped when the *meaning* of stored data changes even though the
  SQLite layout did not (e.g. a tokenizer rule change, a parser
  emitting qualnames differently, an edge resolver pass that
  rewrites stored values). Mismatch is **soft**: `Open()` succeeds,
  search keeps working with the older data, and the difference is
  surfaced through `codemap status` (`stale = true` plus a
  one-line `stale_reason`) so the user can decide when to run
  `codemap reindex`.

The split keeps existing scripts and agent integrations alive across
indexer-only changes (which historically would have required a hard
reindex) while still flagging that the index is out of date with the
running binary.

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
              to_qualname        # may be an unresolved bare name; the
                                 # short-range resolver in store rewrites
                                 # bare same-module callees in place
              kind               # call | reference | inherit | import
              resolved : bool
              file               # source file the edge was discovered in;
                                 # used by per-file cascading delete
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
`parse(file) -> ([Symbol], [Edge])`. Files of unknown extension fall back
to whole-file chunking — still searchable via L0 over content.

Languages with full extraction (functions, methods, classes, variables,
imports, constants + call/reference/inherit/import edges) as of v0.1.x:
Python, Java, JavaScript, TypeScript, TSX, C#, C++. The `golang` and
`rust` adapter packages exist as scaffolds (4-line stubs) so the
registry stays uniform; symbols in those languages currently come from
the file-level fallback. Filling them in is post-v1 follow-up.

### 9.3 Indexer
For each changed file: parse → emit symbols and edges → tokenize for BM25 →
(optional) embed with the encoder → upsert into SQLite inside one
transaction. Bumps `Meta.indexed_at`.

### 9.4 Store
Single-file SQLite per repo (§7.1). One global registry TOML.

### 9.5 Search
1. Tokenize the query the same way the index was built (CamelCase /
   snake_case split, lowercased). Short query terms (≥ 3 chars) are
   prefix-expanded against stored tokens so e.g. `parse` matches
   stored `parser`/`parsed` (PR #5; `tokens.token LIKE 'parse%'`).
2. BM25 over `tokens` → top-50 with structural filters
   (`--kind`, `--scope`, `--file`).
3. If `--rerank` and an encoder is available: dense re-rank against
   `symbols.vec` → top-10.
4. Emit JSON:
   `[{file, line_start, line_end, qualname, kind, scope, snippet, score, indexed_at}]`.

There is no separate "natural language vs identifier" path inside
codemap — every query goes through the same lexical pipeline.
Translating prose into identifier-shaped queries is the agent's job
(see §4.4 / SKILL.md).

### 9.6 Graph queries — `refs` (incoming) and `calls` (outgoing)
Symmetric pair, both backed by the `edges` table.
- `codemap refs <qualname>` — every site that references the symbol.
- `codemap calls <qualname>` — every symbol the given function/method
  invokes or references in its body.
This pair is what makes the example query "what functions does
`main.init` call?" answerable in one round trip.

### 9.7 Visualizer
Single static `graph.html`. One CDN dep at view time (vis-network).
Header shows `lastIndexed`, repo path, and node/edge counts; v0.1.x
also adds a floating search box, focus-mode edges (drawn only for
the selected node), an aside detail panel with grouped outgoing
references, dark mode, and selection-history nav with camera
follow. Render-time filters: `--file` glob, `--kind` set.

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
| `codemap install-self` | Copy the running binary to `~/.codemap/bin/codemap{,.exe}` and persist that directory on the user PATH (HKCU\Environment on Windows; a marker block in `~/.bashrc` / `~/.zshrc` / `~/.config/fish/config.fish` / `~/.profile` on Unix). Idempotent; no admin rights needed. |
| `codemap uninstall-self` | Reverse `install-self`: remove the binary and strip the PATH change. |
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

| Channel | Status (v0.1.x) |
|---|---|
| GitHub Releases — `linux-amd64`, `linux-arm64`, `windows-amd64` | **primary**; published on every `v*` tag via GoReleaser. |
| GitHub Releases — darwin amd64/arm64 | **deferred**; macOS cross-builds were unstable under the current zig-cc toolchain and are temporarily disabled in `.goreleaser.yml`. Re-enable once a reliable mac path is confirmed. |
| GitHub Releases — windows arm64 | deferred; zig windows arm64 support is still maturing. |
| `go install github.com/devchan97/code-map/cmd/codemap@latest` | yes — works wherever the user has Go 1.25+ and a C compiler available locally. |
| `codemap install-self` (post-download PATH setup) | yes — what most release-zip users run after extracting the archive. See §10 and `internal/install/`. |
| Homebrew tap | **not yet**; the formula exists in earlier drafts but the `devchan97/homebrew-tap` repo is not published. Will return alongside darwin builds. |
| `curl … \| sh` bootstrapper | not implemented; `install-self` covers the same use case without an extra hosted script. |
| Scoop / winget / apt / dnf | future work. |

CI: GitHub Actions + GoReleaser on a single `ubuntu-latest` runner.
tree-sitter CGO is handled by zig-cc cross-builds so the published
artifacts are fully static and end users do not need a local C
toolchain unless they go through `go install`.

---

## 13. Repository Layout

```
codemap/
├── cmd/codemap/                 # main package, CLI entrypoint
├── internal/
│   ├── cli/                     # cobra subcommand handlers (thin shims)
│   ├── core/                    # domain types + sentinel errors
│   ├── walker/                  # filesystem enumeration + secrets-aware ignore
│   ├── parser/                  # tree-sitter adapter registry
│   │   ├── python/              # Python (CGO)
│   │   ├── java/                # Java (CGO)
│   │   ├── ts/                  # JavaScript / TypeScript / TSX (CGO)
│   │   ├── csharp/              # C# (CGO)
│   │   ├── cpp/                 # C++ (CGO)
│   │   ├── fallback/            # whole-file fallback for unknown languages
│   │   ├── golang/              # scaffold (post-v1)
│   │   └── rust/                # scaffold (post-v1)
│   ├── lexical/                 # BM25 + tokenization
│   ├── encoder/                 # optional ONNX rerank (build tag: encoder)
│   ├── store/                   # SQLite gateway (modernc.org/sqlite, pure Go)
│   ├── registry/                # ~/.codemap/registry.toml + 6-step resolver
│   ├── pipeline/                # init / index / reindex orchestration
│   ├── search/                  # search.Run + Show
│   ├── graph/                   # refs + calls
│   ├── visualize/               # graph.html renderer
│   ├── skill/                   # SKILL.md installer
│   ├── install/                 # codemap install-self / uninstall-self
│   └── platform/                # OS abstraction (paths, atomic write, browser)
├── skill/SKILL.md.tmpl          # canonical SKILL.md template
├── scripts/                     # zig-cc wrappers (one per release target)
├── .github/
│   ├── assets/                  # README hero GIF and other media
│   └── workflows/               # ci.yml + release.yml
├── codemap-design.md            # this document
├── architecture.md              # module-level architecture
├── README.md / README-ko.md
└── go.mod / go.sum
```

User runtime layout:

```
~/.codemap/
├── registry.toml                # all known indexes (per §6.2)
├── state.toml                   # optional: default_repo
└── bin/                         # populated by `codemap install-self`
    └── codemap{,.exe}           # the running binary, on user PATH

<each-repo>/.codemap/
├── index.db                     # SQLite (per §7.1)
└── graph.html                   # written by `codemap visualize`
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
| M6 | Cross-platform releases via zig-cc + GoReleaser | ✅ for linux amd64/arm64 + windows amd64; darwin and windows arm64 deferred. Homebrew tap pending the darwin path. |
| post-M6 | v0.1.x patch line — `install-self` for one-shot PATH setup, `indexer_ver` staleness flag, short-range edge resolver, BM25 prefix expansion, status/list count consistency, visualize UX (search box, focus-mode edges, dark mode, aside detail panel, selection history with camera follow, grouped outgoing references). | ✅ (PRs #4–#14 against v0.1.0 → v0.1.4) |

---

## 15. Open Questions

Status as of v0.1.x. Resolved items moved to §16; remaining items are
either deferred (cost outweighs current value) or blocked on upstream
work outside this repo.

### Resolved (see §16 for the final decision)

- **Q1 — tree-sitter via CGO vs WASM/purego.** Resolved: **CGO**.
  zig-cc cross-compilation in `release.yml` makes this invisible to
  end users (single static binary per target). WASM revisited only if
  cross-build itself becomes a maintenance burden.
- **Q2 — BM25 hand-rolled vs Bleve.** Resolved: **hand-rolled** (`internal/lexical/bm25.go`,
  ~64 lines). Confirmed in M1 and validated again by PR #5
  (query-time prefix expansion was a one-file change; not feasible if
  Bleve owned the index).
- **Q6 — secrets policy.** Resolved: implemented in
  `internal/walker/secrets.go`. Basename match (`.env`), prefix match
  (`.env.*`), extension/keyfile match (`*.pem`, `*.key`, `id_rsa*`,
  `id_ed25519*`), plus content scanning for AWS access-key prefix and
  PEM headers. User overrides via `.codemapignore` (gitignore syntax)
  or `walker.Options.ExtraIgnores` for embedders.
- **Q7 — monorepo strategy.** Resolved: **per repo root**, with the
  registry providing the multi-index handle. A user who wants finer
  granularity runs `codemap init <subdir>` for each subproject;
  resolution still works because the registry stores absolute paths.

### Still open

- **Q3 — edge resolution depth for dynamic languages.** Currently
  parsers emit `to_qualname` as the raw text seen at the call site
  (e.g. `_cleanup` rather than `module._cleanup`). Same-module
  unqualified references therefore stay unresolved and `refs`/`calls`
  on the canvas drop them. A short-range resolver (try the current
  module's prefix, then transitively-imported aliases) would fix the
  80% case without paying for a real type system. Type-aware analysis
  is explicitly **not** on the table — it would push codemap out of
  the "shallow but cheap" bucket and into IDE territory.
- **Q4 — visualization at scale.** vis-network has been validated up
  to ~2k nodes / ~2.5k edges (CC-Pilot fixture) with the
  focused-edge-mode pattern from PR #4 (edges hidden by default,
  shown only for the selected node). 10k+ is unverified. Likely
  remediation if it shows up: cytoscape.js with progressive
  disclosure / community detection, or simply `--file <glob>` to
  render a slice. No action until a real >5k repo lands as a user
  report.
- **Q5 — SKILL spec drift.** Claude Code paths and frontmatter are
  pinned (`internal/skill/paths.go` + golden test). Codex still
  pending upstream; `--agent codex` is rejected with a single
  user-readable line and `--print` still works for manual placement.
- **Q8 — cross-repo search (`--repo a,b,c`).** Deferred to post-v1.
  SQL union over multiple stores is mechanical, but BM25 statistics
  (avgLen, idf) are corpus-local, so the ranking step needs design
  work before this is honest. Revisit when an actual user has a
  multi-repo workflow that the per-repo workflow can't cover.

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
