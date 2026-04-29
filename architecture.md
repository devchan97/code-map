# codemap — Architecture Document (v1.0)

> This document is the implementation blueprint built on top of
> `codemap-design.md` (v0.3 FINAL). The design doc answers *what* we are
> building; this doc answers *how the code is wired together* — module
> boundaries, data flow, public interfaces, and dependency rules. The work

---

## 0. Operating Principles (only the ones that drive code)

1. **codemap is a pure retrieval layer.** No LLM call ever. All reasoning
   lives in the calling agent (Claude Code / Codex). → No package may pull in
   an LLM client.
2. **Two-tier storage: per-repo SQLite + a global TOML registry.** No server,
   no daemon. All I/O is local files.
3. **Cold start ≤ 50 ms is a hard constraint.** → Lazy initialisation; heavy
   dependencies (ONNX, etc.) live behind build tags. Indexing and search must
   not share boot paths.
4. **Tree-sitter (CGO) is the v1 default.** Language parsers are isolated
   behind an adapter pattern. WASM (`purego`) is deferred (Open Question 1).
5. **Lexical BM25 is on by default.** Encoder rerank is gated behind a build
   tag, so the default binary contains no ONNX dependency.
6. **Atomic writes.** An indexing run completes inside a single SQLite
   transaction. Mid-run failures must leave the previous state intact.
7. **Agent-friendly JSON.** Every command supports `--json`. Human-readable
   output is the default; the JSON shape is a stable contract.

---

## 1. System Context Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│ External actors                                                 │
│   • Human user           (running codemap directly in a shell)  │
│   • Coding agent         (Claude Code / Codex via SKILL.md)     │
└──────────────────────────┬──────────────────────────────────────┘
                           │ argv + stdout/stderr (text|json)
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│ codemap CLI process — single static binary, written in Go       │
│                                                                 │
│   cmd/codemap (entrypoint)                                      │
│    └─ cobra root → subcommand dispatcher                         │
│        ├─ init / index / reindex / forget / list / status        │
│        ├─ search / show / refs / calls                           │
│        ├─ visualize                                              │
│        ├─ install-skill / uninstall-skill                        │
│        └─ version                                                │
│                                                                 │
│   internal/* (domain modules — see §3)                           │
└──────────┬──────────────────────────────┬───────────────────────┘
           │                              │
           ▼                              ▼
┌──────────────────────┐         ┌──────────────────────────┐
│ ~/.codemap/          │         │ <repo>/.codemap/         │
│   registry.toml      │         │   index.db    (SQLite)   │
│   state.toml (opt)   │         │   meta.json   (mirror)   │
│                      │         │   config.toml (opt)      │
│                      │         │   graph.html  (visualize)│
└──────────────────────┘         └──────────────────────────┘
```

- A new process is spawned per invocation (no daemon). Therefore **all state
  is on disk.** Nothing is held in memory across invocations.
- Default builds make zero outbound network calls. The CDN script tag inside
  the rendered `graph.html` is loaded by the user's browser, not by codemap.

---

## 2. Directory & Package Layout (Go)

The skeleton mirrors `codemap-design.md` §13. Each package's responsibility
and public surface is fixed below.

```
codemap/
├── cmd/
│   └── codemap/
│       ├── main.go              # entrypoint; exit-code mapping only
│       └── root.go              # cobra root command registration
│
├── internal/
│   ├── cli/                     # cobra handlers per subcommand (thin)
│   │   ├── init.go
│   │   ├── index.go
│   │   ├── reindex.go
│   │   ├── list.go
│   │   ├── status.go
│   │   ├── forget.go
│   │   ├── search.go
│   │   ├── show.go
│   │   ├── refs.go
│   │   ├── calls.go
│   │   ├── visualize.go
│   │   ├── skill.go             # install-skill / uninstall-skill
│   │   ├── install_self.go      # install-self / uninstall-self
│   │   ├── version.go
│   │   ├── flags.go             # shared flags (--json, --repo, --top …)
│   │   └── output.go            # text / json formatters
│   │
│   ├── core/                    # domain types + errors (no deps)
│   │   ├── types.go             # File, Symbol, Edge, Meta, SearchHit …
│   │   ├── kinds.go             # SymbolKind, EdgeKind, Scope enums
│   │   └── errors.go            # sentinel + typed errors
│   │
│   ├── walker/                  # filesystem enumeration
│   │   ├── walker.go            # ignore rules + sha1
│   │   ├── ignore.go            # .gitignore + .codemapignore parsing
│   │   └── secrets.go           # auto-skip .env, *.pem, *.key, …
│   │
│   ├── parser/                  # tree-sitter adapters
│   │   ├── parser.go            # Parser interface + registry
│   │   ├── language.go          # extension → language mapping
│   │   ├── python/
│   │   ├── ts/                  # JavaScript, TypeScript, TSX (shared grammar set, M3)
│   │   ├── java/                # Java (M3)
│   │   ├── csharp/              # C# (M3)
│   │   ├── cpp/                 # C++ (M3)
│   │   ├── golang/
│   │   ├── rust/
│   │   └── fallback/            # whole-file chunk for unknown languages
│   │
│   ├── lexical/                 # BM25 + tokenization (pure Go)
│   │   ├── tokenize.go          # CamelCase / snake_case decomposition
│   │   ├── bm25.go              # scoring
│   │   └── index.go             # SQLite tokens-table builder + queries
│   │
│   ├── encoder/                 # optional ONNX rerank (build tag)
│   │   ├── encoder.go           # interface
│   │   ├── onnx_enabled.go      // +build encoder
│   │   └── onnx_stub.go         // +build !encoder
│   │
│   ├── store/                   # SQLite gateway
│   │   ├── schema.go            # DDL + schema_ver constant
│   │   ├── store.go             # Open/Close/WithTx
│   │   ├── files.go             # files table CRUD
│   │   ├── symbols.go
│   │   ├── edges.go
│   │   ├── tokens.go
│   │   ├── meta.go
│   │   └── migrate.go           # schema_ver check + reject on skew
│   │
│   ├── registry/                # ~/.codemap/registry.toml
│   │   ├── registry.go          # Load / Save / Upsert / Remove
│   │   ├── resolve.go           # 6-step resolution from §6.2
│   │   └── state.go             # state.toml (default_repo)
│   │
│   ├── pipeline/                # indexing orchestrator
│   │   ├── pipeline.go          # walker → parser → store, tx boundary
│   │   ├── diff.go              # sha1 comparison; changed-file queue
│   │   └── progress.go          # tty progress + summary
│   │
│   ├── search/                  # search orchestrator
│   │   ├── search.go            # L0 → optional L1 rerank → SearchHit
│   │   ├── filters.go           # --kind --scope --file
│   │   └── show.go              # qualname / file:line dispatcher
│   │
│   ├── graph/                   # refs / calls queries
│   │   ├── refs.go
│   │   └── calls.go
│   │
│   ├── visualize/               # graph.html renderer
│   │   ├── render.go
│   │   └── template.go          # embed.FS for HTML template
│   │
│   ├── skill/                   # SKILL.md install / uninstall
│   │   ├── install.go
│   │   ├── paths.go             # per-agent path rules; ErrCodexPending sentinel
│   │   └── template.go          # SKILL.md.tmpl embed
│   │
│   ├── install/                 # `codemap install-self` / `uninstall-self`
│   │   ├── install.go           # copy binary to ~/.codemap/bin, ensure PATH
│   │   ├── path_windows.go      # HKCU\Environment + WM_SETTINGCHANGE
│   │   └── path_unix.go         # marker block in ~/.bashrc / ~/.zshrc / …
│   │
│   └── platform/                # OS abstraction
│       ├── paths.go             # ~/.codemap, %USERPROFILE% handling
│       ├── fsatomic.go          # atomic rename, lock files
│       └── tty.go               # color/progress capability detection
│
├── skill/
│   └── SKILL.md.tmpl            # //go:embed source of truth
│
├── scripts/                     # zig-cc wrappers, one per release target
│
├── codemap-design.md            # design doc
├── architecture.md              # this document
│
├── .github/
│   ├── assets/                  # README hero GIF and other media
│   └── workflows/
│       ├── ci.yml               # test + lint + build matrix
│       └── release.yml          # GoReleaser + zig-cc cross
│
├── go.mod / go.sum
└── README.md / README-ko.md
```

### 2.1 Dependency Rules (enforced)

```
cli ──► search / pipeline / graph / visualize / skill / registry
        │
        ▼
   store ◄── pipeline, search, graph, visualize
   parser ◄── pipeline
   walker ◄── pipeline
   lexical ◄── pipeline (write), search (read)
   encoder ◄── search (optional, build tag)
   registry ◄── cli, pipeline, search
   core    ◄── (importable from anywhere; depends on nothing internal)
   platform ◄── almost every module
```

- `core` imports nothing internal. It holds pure types and errors.
- `cli` contains no domain logic. Each handler does (1) parse args,
  (2) call domain, (3) format output. Tests live in domain packages; CLI
  tests are golden output checks only.
- `parser/*` does not know about `store`. Adapters emit `core.Symbol` /
  `core.Edge` only. This keeps adapter tests isolated and makes adding
  languages cheap.
- The `encoder` package is split by build tag. The default binary does not
  link the ONNX runtime — protecting binary size and cold start.

### 2.2 External Library Candidates (pre-decision)

| Area | Candidate | Notes |
|---|---|---|
| CLI | `spf13/cobra` + `spf13/pflag` | Standard. |
| TOML | `BurntSushi/toml` | Stable. |
| SQLite | `modernc.org/sqlite` (pure Go) | Locked in M1. tree-sitter's CGO requirement is local to `internal/parser/<lang>/`; everything else (store, cli, tests) builds CGO-free with this driver. See §14.A for the trade-off. |
| Tree-sitter | `smacker/go-tree-sitter` (CGO) | One submodule per grammar. |
| Gitignore | `sabhiram/go-gitignore` | Same engine handles `.codemapignore`. |
| BM25 | hand-rolled in `lexical` | A full-stack Bleve is overkill (design §15.2). |
| Logging | stdlib `log/slog` | No additional dependency. |
| Embedded assets | `embed.FS` | SKILL template, `graph.html` template. |

---

## 3. Module Responsibilities

For each module: (a) responsibility, (b) public interface sketch,
(c) non-responsibilities. Signatures are illustrative — final shapes may shift
during implementation, particularly around context plumbing and error
wrapping.

### 3.1 `core`

- **Responsibility.** Domain types, enums, shared errors. Zero dependencies.
- **Types (sketch).**
  ```go
  type File struct {
      Path       string    // repo-relative, slash-normalized
      SHA1       string
      IndexedAt  time.Time
      Language   string
      SizeBytes  int64
  }

  type Symbol struct {
      ID             int64
      Name           string
      Qualname       string
      Kind           SymbolKind
      Scope          Scope
      File           string
      LineStart      int
      LineEnd        int
      ParentQualname string
      Docstring      string
      Snippet        string
      Vec            []float32 // populated only when an encoder is active
  }

  type Edge struct {
      FromQualname string
      ToQualname   string
      Kind         EdgeKind   // call | reference | inherit | import
      Resolved     bool
  }

  type Meta struct {
      SchemaVer    int        // SQLite layout version; mismatch = hard error
      IndexerVer   int        // parser/tokenizer/resolver semantics; mismatch = soft (status stale=true)
      RepoRoot     string
      IndexedAt    time.Time
      Embedder     string     // "lexical" | "bge-small" | …
      SymbolCount  int
      FileCount    int
  }

  type SearchHit struct {
      File      string
      LineStart int
      LineEnd   int
      Qualname  string
      Kind      SymbolKind
      Scope     Scope
      Snippet   string
      Score     float64
      IndexedAt time.Time
  }
  ```
- **Non-responsibilities.** I/O, DB access, filesystem operations. No verbs.

### 3.2 `walker`

- **Responsibility.** Yield index candidates under a given root, applying
  `.gitignore` + `.codemapignore` + secret rules. Compute SHA-1 per file.
  Defaults match design §9.1.
- **Public surface.**
  ```go
  type Entry struct {
      Path string  // repo-relative
      SHA1 string
      Size int64
  }

  type Options struct {
      ExtraIgnores   []string
      FollowSymlinks bool
  }

  func Walk(ctx context.Context, root string, opts Options, yield func(Entry) error) error
  ```
- **Non-responsibilities.** Parsing, DB access. SHA-1 *comparison* is the
  job of `pipeline/diff`.

### 3.3 `parser`

- **Responsibility.** Per-file tree-sitter parse → `[]Symbol`, `[]Edge`. A
  registry of language adapters.
- **Public surface.**
  ```go
  type Parser interface {
      Language() string
      Parse(path string, src []byte) (symbols []core.Symbol, edges []core.Edge, err error)
  }

  func Register(p Parser)
  func For(language string) (Parser, bool)
  func DetectLanguage(path string) string
  ```
- **Non-responsibilities.** SHA-1, DB access. Files of unknown extensions
  fall through to the `fallback` adapter, which emits one whole-file Symbol.
- **Language ramp (M1 → M3).** Python (M1) → Java, JavaScript, TypeScript, TSX, C#, C++ (M3) → Go, Rust (deferred). Every adapter
  follows the variable-mapping rules in §8 verbatim.

### 3.4 `lexical`

- **Responsibility.** Tokenization (CamelCase / snake_case + full token),
  building the `tokens` table, BM25 scoring.
- **Public surface.**
  ```go
  type Tokenizer interface {
      Tokenize(text string) []Token
  }
  type Token struct {
      Text   string
      Field  string  // "name" | "qualname" | "docstring" | "snippet"
      Weight float32
  }

  type Index interface {
      Upsert(symbolID int64, toks []Token) error
      Delete(file string) error
      Search(query string, topN int, filters Filters) ([]ScoredID, error)
  }
  ```
- **Non-responsibilities.** Hydrating full Symbol rows from candidate IDs is
  the `search` package's job.

### 3.5 `encoder` (optional)

- **Responsibility.** Wrap the ONNX runtime. Compute dense vectors for the
  query and candidate snippets; rerank by cosine similarity.
- **Public surface (interface always present; implementations split by
  build tag).**
  ```go
  type Encoder interface {
      Name() string
      EncodeQuery(q string) ([]float32, error)
      EncodeBatch(snippets []string) ([][]float32, error)
  }
  func Default() (Encoder, error) // stub build returns "unsupported"
  ```
- **Non-responsibilities.** The rerank algorithm itself lives in `search`.
  `encoder` only computes vectors.

### 3.6 `store`

- **Responsibility.** All read/write against the per-repo SQLite file. Owns
  the DDL. Manages transactions.
- **Public surface.**
  ```go
  type Store struct { /* db handle, repo path */ }

  func Open(repoRoot string) (*Store, error)
  func (s *Store) Close() error
  func (s *Store) WithTx(ctx context.Context, fn func(Tx) error) error

  // Domain methods, exposed via Tx
  type Tx interface {
      UpsertFile(core.File) error
      DeleteFileArtifacts(path string) error  // symbols/edges/tokens
      InsertSymbols([]core.Symbol) ([]int64, error)
      InsertEdges([]core.Edge) error
      InsertTokens(symbolID int64, []lexical.Token) error
      ReadMeta() (core.Meta, error)
      WriteMeta(core.Meta) error
      ListFiles() ([]core.File, error)
  }
  ```
- **Non-responsibilities.** Business decisions (which files to re-parse,
  etc.). Schema migration follows design §7.4 — refuse + advise the user;
  no auto-migration.

### 3.7 `registry`

- **Responsibility.**
  - Read / write / upsert `~/.codemap/registry.toml`.
  - Repo resolution (the 6 steps from design §6.2).
  - Optional `state.toml` (e.g., `default_repo`).
- **Public surface.**
  ```go
  type Entry struct {
      Name        string
      Path        string
      CreatedAt   time.Time
      LastIndexed time.Time
      FileCount   int
      SymbolCount int
      Embedder    string
      SchemaVer   int
  }

  type Registry interface {
      Load() ([]Entry, error)
      Upsert(Entry) error
      Remove(nameOrPath string) error
  }

  // 6-step resolver
  func Resolve(flagRepo string, cwd string, reg Registry, state State) (Entry, error)
  ```
- **Non-responsibilities.** Index integrity (the actual SQLite state) is
  `store`'s job. The registry is a mirror.

### 3.8 `pipeline`

- **Responsibility.** Orchestrates `init` / `index` / `reindex`. Implements
  the flow in design §7.2.
- **Public surface.**
  ```go
  type IndexOptions struct {
      Force      bool   // reindex
      Concurrency int   // parser workers
      Encoder    encoder.Encoder // optional
  }

  type Summary struct {
      ParsedFiles  int
      SkippedFiles int
      Symbols      int
      Edges        int
      Duration     time.Duration
      IndexedAt    time.Time
  }

  func Index(ctx context.Context, repoRoot string, opts IndexOptions) (Summary, error)
  func Reindex(ctx context.Context, repoRoot string, opts IndexOptions) (Summary, error)
  ```
- **Transaction boundary.** A single transaction per "set of changed files":
  `DELETE FROM symbols/edges/tokens WHERE file IN (...)` → INSERT new data →
  `UPDATE meta`. Failure rolls back the whole run.
- **Concurrency.** Parsing fans out to N workers (CPU-bound). DB writes are
  serialised through a single goroutine (SQLite is single-writer).

### 3.9 `search`

- **Responsibility.** Domain logic for `search` and `show`. Composes L0 with
  the optional L1 rerank.
- **Public surface.**
  ```go
  type Query struct {
      Text      string
      TopN      int
      KindFilter  []core.SymbolKind
      ScopeFilter []core.Scope
      FileGlob    string
      Rerank      bool
  }

  func Run(ctx context.Context, s *store.Store, enc encoder.Encoder, q Query) ([]core.SearchHit, error)
  func Show(ctx context.Context, s *store.Store, target string) (core.Symbol, error)
  ```
- **Non-responsibilities.** Query interpretation (NL → tokens). Per design
  §4.4, that is the calling agent's job. codemap takes whatever tokens are
  passed in and runs them.

### 3.10 `graph`

- **Responsibility.** `refs` (incoming) and `calls` (outgoing). Both are
  one-direction joins on the `edges` table.
- **Public surface.**
  ```go
  type Edge struct {
      From core.Symbol
      To   core.Symbol
      Kind core.EdgeKind
      Resolved bool
  }
  func Refs(ctx, store, qualname) ([]Edge, error)
  func Calls(ctx, store, qualname) ([]Edge, error)
  ```

### 3.11 `visualize`

- **Responsibility.** Render `graph.html`. Single static file output. Loads
  `vis-network` / `cytoscape.js` from a CDN at view time. Header shows
  `lastIndexed`, repo path, node/edge counts (design §9.7).
- **Public surface.**
  ```go
  type Options struct { OutPath string; Open bool }
  func Render(ctx, store, opts Options) (path string, err error)
  ```

### 3.12 `skill`

- **Responsibility.** Render and install/uninstall the SKILL.md template.
  Per-agent path rules (design §11.1).
- **Public surface.**
  ```go
  type Target struct {
      Agent string  // "claude-code" | "codex"
      Scope string  // "user" | "project"
      Repo  string  // used when scope == "project"
  }
  func Install(t Target, print bool) (path string, err error)
  func Uninstall(t Target) error
  ```

### 3.13 `cli`

- **Responsibility.** Register cobra commands, parse flags, call into the
  domain, format output. **No logic of its own.**
- **Output formatting.** `output.go` exposes `WriteHuman` and `WriteJSON`.
  All result types are either `core.*` directly or thin CLI view-models.

### 3.14 `platform`

- **Responsibility.** OS abstraction. `~/.codemap` location (POSIX
  `$HOME/.codemap`, Windows `%USERPROFILE%\.codemap`), atomic rename, file
  locking, TTY detection.
- **Why.** The development environment here is Windows, but design examples
  are POSIX-flavoured. Slash normalisation and OS-specific path handling
  belong in one place.

---

## 4. Data Flow

### 4.1 `codemap init <PATH>`

```
cli/init  ─►  registry.Resolve(flag, cwd) → prepare new Entry if needed
          ─►  platform.EnsureDir(<PATH>/.codemap)
          ─►  store.Open(<PATH>) → create schema if missing
          ─►  registry.Upsert(Entry{LastIndexed: zero})
          ─►  pipeline.Index(<PATH>, default opts)        // first index
          ─►  registry.Upsert(Entry{LastIndexed: now, …})
          ─►  output.WriteHuman(Summary)
```

Idempotent. If `.codemap/` already exists, validate the schema and reuse it.
If the registry is missing, recover from the SQLite `meta` table.

### 4.2 `codemap index <PATH>` (incremental)

```
walker.Walk(root)
   └─► (path, sha1, size) stream
            │
            ▼
pipeline.diff(store.Tx.ListFiles(), stream)
   └─► changed[] / unchanged_count
            │
            ▼ (changed only)
parser.For(language).Parse(path, bytes)
   └─► []Symbol, []Edge   (parallel workers, N=GOMAXPROCS)
            │
            ▼ (single writer goroutine)
store.WithTx(func(tx) {
    for f in changed:
        tx.DeleteFileArtifacts(f.path)
        tx.UpsertFile(f)
        ids := tx.InsertSymbols(symbolsOf(f))
        tx.InsertEdges(edgesOf(f))
        for (sid, snippet) in symbolsOf(f):
            toks := lexical.Tokenize(...)
            tx.InsertTokens(sid, toks)
        if encoder != nil:
            vecs := encoder.EncodeBatch(snippets)
            tx.UpsertSymbolVectors(ids, vecs)
    tx.WriteMeta(updatedMeta)
})
            │
            ▼
registry.Upsert(mirror)
            │
            ▼
output.WriteHuman/JSON(Summary)   # files indexed/skipped, lastIndexed
```

- **Edge resolution (`Edge.Resolved`).** First pass resolves only same-file
  call sites. Cross-file resolution runs once after all files are parsed,
  inside the same transaction (qualname → symbol_id lookup).
- **Failure handling.** A parse error on one file does **not** fail the run.
  The file is isolated, logged, and *its* SHA-1 is not updated, so it gets
  retried next time. A transaction-level error rolls back the whole run.

### 4.3 `codemap search <Q>`

```
cli/search ─► registry.Resolve(--repo, cwd) → repoRoot
           ─► store.Open(repoRoot)
           ─► search.Run(store, enc, Query{...})
                ├─ lexical.Search(q, topN=50, filters)        // L0
                ├─ store.HydrateSymbols(ids)                  // full rows
                └─ if rerank && enc != nil:
                       enc.EncodeQuery + cosine vs symbol.Vec
                       → top 10
           ─► output.WriteJSON([]SearchHit)
```

The cold-start critical path: cobra dispatch → registry load → SQLite open
→ BM25 query. M1 measures whether this fits in 50 ms.

### 4.4 `codemap refs / calls <QUALNAME>`

```
graph.Refs(qualname):
    SELECT e.* FROM edges e
    JOIN symbols s_from ON s_from.qualname = e.from_qualname
    JOIN symbols s_to   ON s_to.qualname   = e.to_qualname
    WHERE e.to_qualname = ?
    AND   e.kind IN ('call', 'reference')

graph.Calls(qualname):  # same shape, but e.from_qualname = ?
```

Unresolved edges (`Resolved=false`) appear in the result set, marked.

### 4.5 `codemap visualize`

```
visualize.Render
   ├─ store: fetch all symbols / edges (with optional --file or --kind filters)
   ├─ inject pre-marshaled JSON into graph.html.tmpl via embed.FS
   └─ atomic-write <repo>/.codemap/graph.html → open in browser if --open
```

Large graphs (10k+ nodes) are deferred to design Open Question 4 — v1 just
renders, with filters as the escape hatch.

### 4.6 `codemap install-skill`

```
skill.Install({Agent, Scope, Repo}, print):
   path := skill.paths.Resolve(target)   // e.g. ~/.claude/skills/codemap/SKILL.md
   body := template.Execute(SKILL.md.tmpl, {Version, BinaryName, …})
   if print: stdout
   else: platform.AtomicWrite(path, body)
```

---

## 5. SQLite Schema (concrete)

The logical schema in design §7.1 is materialised as the following DDL.

```sql
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- known keys: schema_ver, indexer_ver, repo_root, indexed_at,
--             embedder, symbol_count, file_count
--
-- schema_ver  : on-disk SQLite layout version. Mismatch is a hard
--               error at Open(); the user must `codemap reindex`.
-- indexer_ver : parser/tokenizer/edge-resolver semantics version.
--               Mismatch is surfaced through `codemap status`
--               (Stale=true) — DB is still readable, search just
--               returns data shaped by an older indexer.

CREATE TABLE files (
    path        TEXT PRIMARY KEY,
    sha1        TEXT NOT NULL,
    indexed_at  TEXT NOT NULL,            -- ISO 8601
    language    TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL
);

CREATE TABLE symbols (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    qualname        TEXT NOT NULL,
    kind            TEXT NOT NULL,        -- function|method|class|variable|import|constant
    scope           TEXT NOT NULL,        -- global|class|local|param
    file            TEXT NOT NULL,
    line_start      INTEGER NOT NULL,
    line_end        INTEGER NOT NULL,
    parent_qualname TEXT,
    docstring       TEXT,
    snippet         TEXT NOT NULL,
    vec             BLOB,                 -- nullable; only when an encoder is active
    FOREIGN KEY(file) REFERENCES files(path) ON DELETE CASCADE
);
CREATE INDEX idx_symbols_file        ON symbols(file);
CREATE INDEX idx_symbols_qualname    ON symbols(qualname);
CREATE INDEX idx_symbols_kind_scope  ON symbols(kind, scope);

CREATE TABLE edges (
    from_qualname TEXT NOT NULL,
    to_qualname   TEXT NOT NULL,
    kind          TEXT NOT NULL,          -- call|reference|inherit|import
    resolved      INTEGER NOT NULL,       -- 0/1
    file          TEXT NOT NULL,          -- file the edge was discovered in
    FOREIGN KEY(file) REFERENCES files(path) ON DELETE CASCADE
);
CREATE INDEX idx_edges_to    ON edges(to_qualname);
CREATE INDEX idx_edges_from  ON edges(from_qualname);
CREATE INDEX idx_edges_file  ON edges(file);

CREATE TABLE tokens (
    symbol_id  INTEGER NOT NULL,
    token      TEXT NOT NULL,
    field      TEXT NOT NULL,             -- name|qualname|docstring|snippet
    weight     REAL NOT NULL,
    FOREIGN KEY(symbol_id) REFERENCES symbols(id) ON DELETE CASCADE
);
CREATE INDEX idx_tokens_token  ON tokens(token);

PRAGMA journal_mode = WAL;
PRAGMA synchronous  = NORMAL;
PRAGMA foreign_keys = ON;
```

- The `edges.file` column does not appear in design §7.1 — it is added here
  to support the per-file cascading delete from §7.2 (`DELETE WHERE file IN
  changed`). `symbols` already carries `file` for the same reason.
- `vec` BLOB layout: `[]float32` little-endian, 4-byte length header followed
  by the payload. Encode/decode helpers live in the `encoder` package.

---

## 6. Config / Flags / Output Contract

### 6.1 Common Flags

| Flag | Applies to | Meaning |
|---|---|---|
| `--repo NAME\|PATH` | search/show/refs/calls/visualize/status/index/reindex | Explicit repo. Either a registry NAME or an absolute/relative PATH. |
| `--json` | All output commands | Emit JSON to stdout. Stable contract. |
| `--top N` | search | Default 10. |
| `--kind` | search | Comma-separated, e.g. `function,method`. |
| `--scope` | search | Comma-separated. |
| `--file GLOB` | search | doublestar glob. |
| `--rerank` | search | Only meaningful in `encoder` builds; warn and continue otherwise. |
| `--out PATH` | visualize | Output path for `graph.html`. Defaults to `<repo>/.codemap/graph.html`. |
| `--open` | visualize | Open in the OS default browser. |
| `--scope user\|project`, `--agent claude-code\|codex`, `--print` | install-skill | See design §11. |

### 6.2 JSON Output Schemas (stable)

#### `codemap search --json`
```json
[
  {
    "file": "src/api/middleware.py",
    "line_start": 142,
    "line_end": 178,
    "qualname": "api.middleware.apply_rate_limit",
    "kind": "function",
    "scope": "global",
    "snippet": "def apply_rate_limit(req): ...",
    "score": 12.34,
    "indexed_at": "2026-04-28T05:30:11Z"
  }
]
```

#### `codemap list --json`
```json
[
  {
    "name": "myproject",
    "path": "/Users/me/work/myproject",
    "created_at": "...",
    "last_indexed": "...",
    "file_count": 312,
    "symbol_count": 4823,
    "embedder": "lexical",
    "schema_ver": 1
  }
]
```

#### `codemap status --json`
```json
{
  "name": "myproject",
  "path": "/Users/me/work/myproject",
  "last_indexed": "...",
  "file_count": 312,
  "symbol_count": 4823,
  "embedder": "lexical",
  "schema_ver": 1,
  "indexer_ver": 2,
  "stale": false,
  "stale_reason": ""
}
```

`indexer_ver` is bumped when the parser/tokenizer/edge-resolver
semantics change without altering the SQLite layout. `stale` becomes
true (and `stale_reason` is populated with a one-line user-facing
message) whenever the on-disk indexer_ver differs from the running
binary's value, including the legacy case of a pre-tracking index
(`indexer_ver = 0`). The DB is always readable; the field is a hint
to run `codemap reindex`.

#### `codemap refs --json` / `codemap calls --json`
```json
[
  {
    "from": {"qualname": "...", "file": "...", "line_start": 1, "line_end": 1},
    "to":   {"qualname": "...", "file": "...", "line_start": 1, "line_end": 1},
    "kind": "call",
    "resolved": true
  }
]
```

This contract is what SKILL.md depends on. Any change requires updating both
the design doc and the SKILL template.

### 6.3 Exit Codes

- 0 — success
- 1 — generic error
- 2 — usage error (cobra default)
- 3 — repo could not be resolved
- 4 — schema version mismatch (recommend `codemap reindex`)
- 5 — corrupt index

This is a stable contract so an agent can branch on the exit code of
`codemap status`, etc.

---

## 7. Concurrency / Performance / Resource Model

| Area | Policy |
|---|---|
| Parser workers | Default `runtime.NumCPU()`, override with `--concurrency N` |
| DB writer | Single goroutine (SQLite single writer); fed via channel |
| File I/O | Walker streams; no full tree held in memory |
| Memory ceiling | Per-file parse output is held in memory only until the transaction commits, then released |
| Large files | Files > 1 MiB are skipped by default (configurable) — protects token spend |
| Goroutine leaks | Context cancellation propagates to walker, parser, and writer |
| WAL mode | `journal_mode=WAL` allows reader/writer concurrency (e.g. `status` reads while `index` writes) |

The 50 ms cold-start target (R7/R8) drives the import strategy: heavyweight
domain packages must be reachable only from the specific cobra `RunE`
function that needs them, not from the file scope of `cli/*.go`. Pulling
every domain package at the cli root forces every command to pay for every
import.

---

## 8. Testing Strategy

| Module | Test type | Key cases |
|---|---|---|
| core | unit | enum (un)marshalling; JSON tag stability |
| walker | unit + fixtures | `.gitignore` precedence; secret skip; symlinks |
| parser/python | unit | every variable-mapping case from design §8 |
| parser/ts, go, rust | unit | same matrix per language |
| lexical | unit + property | CamelCase / snake_case decomposition; BM25 monotonicity |
| store | unit + tx | cascading delete; concurrent reader/writer (WAL) |
| registry | unit | every branch of the 6-step resolver |
| pipeline | integration | mini-repo fixture: init → index → reindex cycle |
| search | integration | filter combinations; `--rerank` with stub encoder |
| graph | integration | refs/calls in both directions; unresolved edges |
| visualize | snapshot | header metadata correctness |
| skill | golden | template render output |
| cli | golden output | text + JSON for each command |

End-to-end integration runs in GitHub Actions matrix (linux, macOS, windows).
The codemap repo indexes itself in CI; `search "tokenize"` must return the
expected hit.

---

## 9. Build / Release / Distribution

- **GoReleaser + zig-cc** for cross compilation. Tree-sitter CGO is statically
  linked through zig-cc.
- **Build matrix.** darwin amd64/arm64, linux amd64/arm64, windows amd64.
  windows arm64 is deferred for the first release.
- **Build tags.**
  - default: small, fast binary; encoder stub.
  - `encoder`: bundles the ONNX runtime — distributed as a separate artifact
    (`codemap-encoder`) or, in v1, as a placeholder. Users only need it when
    they first turn on `--rerank`.
- **Homebrew tap, `go install`, `curl|sh`** as in design §12.
- **Version surface.** `codemap version` prints (a) git SHA + tag,
  (b) `schema_ver`, (c) embedder name. SKILL/agent compatibility checks rely
  on those three.

---

## 10. Security / Privacy

- **Local only.** The default build makes no outbound network calls. The CDN
  script tag inside the rendered `graph.html` is loaded by the user's
  browser, not by codemap.
- **Skip secrets.** `.env`, `*.pem`, `*.key`, `id_rsa*`, `id_ed25519*`, and
  files whose body matches obvious secret patterns (e.g. `AKIA[0-9A-Z]{16}`)
  are excluded. The policy from design §15.6 lives in
  `walker/secrets.go`.
- **Input safety.** SKILL.md tells agents how to invoke the CLI, but codemap
  reads only `argv` — never stdin. All search terms reach SQLite via
  parameter binding, so SQL injection is impossible.
- **Visibility.** `<repo>/.codemap/` is added to `.gitignore` on `init` by
  default (opt-out flag available).

---

## 11. Extension Points (held open inside v1)

| Extension | Where it plugs in |
|---|---|
| New language | `internal/parser/<lang>/` + `parser.Register` |
| New embedder | Implement `encoder.Encoder` behind a build tag |
| Alternative retrieval backend (FTS5 etc.) | Second implementation of `lexical.Index` |
| Cross-repo search (post-v1) | Extend `search.Run` to take `[]repoRoot` |
| New agent SKILL | New case in `skill/paths.go` |
| Bump indexer semantics (parser / tokenizer / resolver change) | Bump `store.IndexerVer`; users see `stale=true` on next `codemap status` |
| Self-install destination / PATH wiring | `internal/install/path_<os>.go`; one file per platform |

Anything not on this list is intentionally out of scope for v1 — no
monitoring, metrics, or plugin loader.

---

## 12. Milestone Mapping (design §14 → modules)

| Milestone | Status | Modules in scope | Acceptance |
|---|---|---|---|
| M1 | ✅ Done | cli/{init,index,reindex,list,status,forget,search,show,refs,calls}, core, walker, parser/python, lexical, store, registry, pipeline, platform | Useful results on a real Python repo; multi-repo registry works |
| M2 | ✅ Done | visualize, cli/visualize, lastIndexed surfaced | Static HTML; header populated with lastIndexed, repo path, node/edge counts. Subsequent v0.1.x added search box, focus-mode edges, dark mode, aside detail panel, and selection-history nav. |
| M3 | ✅ Done | parser/java, parser/ts (JS+TS+TSX), parser/csharp, parser/cpp | Each language yields correct symbol/edge output on a fixture file; polyglot repo indexes cleanly |
| M4 | ✅ Done | skill, cli/install-skill, SKILL.md template + golden test, drift guard | Template renders byte-exact; install/uninstall round-trip verified. Codex agent target is rejected with a single user-readable message until upstream spec stabilises. |
| M5 | ✅ Done (placeholder) | encoder (build tag), search rerank path | Both build modes compile; nil-encoder fallback returns BM25 cleanly. Real ONNX session is a one-file swap in `onnx_enabled.go`. |
| M6 | ✅ Done (release pipeline) | release.yml, GoReleaser config, scripts/zigcc-* wrappers, install/uninstall-self | Single-runner zig-cc cross-compile; `v*` tag triggers GoReleaser. Linux amd64/arm64 + Windows amd64 ship every release; darwin and Windows arm64 are deferred (.goreleaser.yml). End users get on PATH via `codemap install-self` (no admin) or `go install`. |
| post-M6 | ongoing | indexer_ver, short-range edge resolver, status count consistency, BM25 prefix expansion, install-self, UX polish | v0.1.x patch releases (#5–#12). Issues #1/#2/#3 closed; design.md §15 Q1/Q2/Q6/Q7 resolved, Q3 short-range fix landed. |

---

## 13. Explicit Non-Goals

- A self-hosted LLM, third-party embedding API, RAG-style chunking, or any
  natural-language interpretation inside codemap.
- A daemon / server mode. Cache-warming background processes. Automatic
  schema migrations.
- Multi-machine index sharing or sync. (The registry is local-only.)
- Multi-user permission model. (Single user, single home directory.)
- A plugin loader. Adding a language or an embedder requires a code change
  and a rebuild.

---

## 14. Resolved Decisions

A. **SQLite driver — `modernc.org/sqlite` (pure Go).** The original
   note here said `mattn` because tree-sitter is already CGO; the
   actual M1 implementation chose `modernc.org/sqlite` so that any
   piece of the codebase that does *not* import a tree-sitter parser
   (e.g. `internal/store`, `internal/cli`, the entire test suite) can
   build and run without a C toolchain. tree-sitter's CGO requirement
   is local to `internal/parser/<lang>/`. Decision: keep modernc.
B. **BM25 backend — custom `tokens` table.** Started custom in M1 and
   stayed there: hand-rolled BM25 in `internal/lexical` plus the
   inverted index in SQLite. PR #5's query-time prefix expansion was
   a single-file change because of this; FTS5 would have made the
   same change considerably harder. No fallback needed.
C. **Edge resolution depth — exact match plus a narrow same-module
   prefix rewrite.** v1 marked an edge `resolved=true` only on exact
   qualname match (everything else, including same-module bare-name
   calls in dynamic languages, stayed unresolved). PR #11 added a
   second pass: for an unresolved edge whose `to_qualname` has no
   dot, prepend the `from_qualname`'s module prefix and check
   whether that produces a known qualname. Type inference,
   transitive imports, aliasing remain explicit non-goals — design
   philosophy keeps codemap shallow but cheap rather than reaching
   for IDE-level accuracy. The bump landed under `IndexerVer = 2`.
D. **Windows path normalisation.** Every stored path is forward-slash;
   OS conversion happens only in `platform.PathFromRel`. The dev
   environment here is Windows, so this is enforced from day one.
E. **Tree-sitter grammar pinning.** `go.mod` pins explicit SHAs. The
   first M1 PR locked them in; subsequent bumps go through `go get`
   like any other dependency.