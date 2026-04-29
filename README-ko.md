# codemap

> [English README →](./README.md)

코딩 에이전트 (Claude Code, Codex) 를 위해 레포지토리를 per-repo SQLite 인덱스에
색인하고 정확한 `file:line` 위치를 돌려주는 오픈소스 CLI 입니다.
**LLM 없음** — codemap 은 순수 retrieval layer 이며, 모든 추론은 호출하는
에이전트가 담당합니다.

![codemap visualize — 검색·포커스·다크모드를 갖춘 인터랙티브 심볼 그래프](.github/assets/preview.gif)

```
agent  →  "rate-limit 로직 어디 있어?"
codemap →  src/api/middleware.py:142-178   func apply_rate_limit
            (caller 4건, 색인 시각 2026-04-28 18:12 KST)
agent  →  1,800 줄 통째로 읽지 않고 36 줄만 읽음
```

## 왜 필요한가

에이전틱 코딩에서 토큰 비용을 가장 많이 잡아먹는 것은 **파일 통째 읽기** 입니다.
에이전트는 관련 코드의 위치를 모르기 때문에 30 줄을 찾기 위해 500–2,000 줄짜리
파일을 통째로 끌어옵니다. codemap 은 식별자와 `file:line` 범위를 돌려주는 작은
조회 프리미티브를 제공해서, 에이전트가 부분 읽기 (partial read) 만으로 일을
끝낼 수 있게 합니다.

## 핵심 특징

- **per-repo SQLite 인덱스** (`<repo>/.codemap/index.db`) + **글로벌 레지스트리**
  (`~/.codemap/registry.toml`). 서버도, 데몬도, 백그라운드 프로세스도
  없습니다 — 매 호출마다 새 프로세스이고 콜드 스타트 예산은 50 ms 이하입니다.
- **BM25 lexical 검색** 이 기본 — 직접 구현, ML 의존성 0, 식별자 형태의
  질의에 빠릅니다 (실제 워크로드의 대부분이 이쪽입니다). 선택적 encoder
  rerank 는 `-tags encoder` 뒤에 격리되어 있어 기본 바이너리는 가볍게
  유지됩니다.
- **Tree-sitter 파서** 가 함수 / 메서드 / 클래스 / 변수 / 임포트 / 상수와
  call / reference / inherit / import 엣지를 추출합니다. 지원 언어:
  Python, Java, JavaScript, TypeScript, TSX, C#, C++. Go / Rust 는 스캐폴딩만
  되어 있습니다.
- **SKILL.md 자동 설치** — `codemap install-skill` 로 Claude Code (그리고
  스펙이 확정되면 Codex) 가 파일 통째 읽기 전에 codemap 을 먼저 호출하도록
  설치합니다.
- **원샷 셀프 인스톨** — `codemap install-self` 가 바이너리를
  `~/.codemap/bin` 에 복사하고 사용자 PATH 에 영구 등록합니다 (Windows 는
  HKCU, Unix 는 셸 rc 파일). 관리자 권한 필요 없습니다.
- **단일 정적 바이너리** — 릴리스 아티팩트는 zig-cc 로 크로스 컴파일되어
  나오므로, tree-sitter 가 내부적으로 CGO 를 쓰는데도 사용자 머신에 C
  툴체인이 필요하지 않습니다.
- **로컬 only.** 기본 빌드는 외부 네트워크 호출이 0건. 인덱스는 머신 밖으로
  나가지 않습니다.

## 개발 철학

codemap 은 무엇인가, 그리고 무엇이 아닌가에 대해 의견이 분명합니다. 같은
트레이드오프가 어떤 워크플로우에는 잘 맞고 어떤 워크플로우에는 잘 안
맞기 때문에, 경계를 먼저 명시합니다.

- **에이전트 우선, 사람 우선이 아님.** 주 사용자는 매 턴마다 codemap 을
  서브프로세스로 호출하는 코딩 에이전트이지, IDE 에서 그래프를 탐색하는
  사람이 아닙니다. CLI 와 안정적인 JSON 계약이 먼저, GUI 는 나중. 인터랙티브
  `graph.html` 은 데이터를 갖고 있는 김에 만든 부산물이지 메인 제품이
  아닙니다.
- **순수 retrieval, LLM 없음.** codemap 은 식별자와 `file:line` 범위를
  돌려줍니다. LLM 을 내장하지 않고, 외부 임베딩 API 도 호출하지 않으며,
  RAG 식 chunking 도, 자연어 해석도 하지 않습니다. 모든 추론은 호출자의
  책임. 비용이 예측 가능하고, 출력이 검증 가능하며, 바이너리가 단순하게
  배포됩니다.
- **재사용 비용 절감 우선, 정확도 깊이는 양보.** 인덱싱은 인크리멘털
  (per-file SHA-1), 변경 없는 재호출은 약 10 ms, `status` 는 SQLite meta
  헤더만 읽습니다. 에이전트가 비용 걱정 없이 매 턴마다 호출할 수
  있습니다. 그 대신 codemap 은 의도적으로 tree-sitter 선에서 멈춥니다 —
  타입 인식 리졸버를 돌리거나 완전한 콜그래프를 빌드하거나, 모든 엣지가
  알려진 심볼로 resolve 된다고 보장하지 않습니다. 그게 필요하면 IDE 나
  language server 를 쓰세요. codemap 은 그것들을 대체하려는 도구가
  아닙니다.
- **지루하고 의존성 적은 기술.** SQLite (`modernc.org/sqlite`, pure
  Go), BM25 직접 구현, ML 의존성은 빌드 태그 뒤에 격리, 플러그인 로더
  없음. 언어 추가는 코드 수정 + 재빌드 입니다.
- **로컬, 단일 사용자, 텔레메트리 없음.** 서버도, 공유 캐시도, 어떤 기본
  코드 경로에서도 머신을 떠나는 데이터가 없습니다.

## 진행 상태

Python + 6 개 언어 파서 (Java, JavaScript, TypeScript, TSX, C#, C++),
`visualize` 와 모든 출력에 `lastIndexed` 노출, SKILL.md 설치기,
원샷 PATH 설정용 `install-self`, `encoder` 빌드 태그 뒤의 인코더 rerank
(ONNX 결선은 한 파일 교체로 가능), zig-cc + GoReleaser 기반 릴리스
파이프라인. linux amd64 / arm64 와 windows amd64 바이너리가 모든
`v*` 태그 푸시마다 게시됩니다. macOS 빌드는 일시 중단 (`.goreleaser.yml`
참고).

## 빌드

**Go 1.25+** 와 **C 툴체인** (tree-sitter CGO 때문) 이 필요합니다.

```sh
go build ./cmd/codemap
```

선택적 encoder rerank (v0.1.x 은 placeholder; ONNX 결선은
`internal/encoder/onnx_enabled.go` 한 파일 교체로 가능):

```sh
go build -tags encoder ./cmd/codemap
```

`Makefile` 단축 타겟:

```sh
make build   # ./bin/codemap 생성
make vet     # go vet ./...
make ci      # vet + build + encoder 빌드
```

## 설치

[Releases 페이지](https://github.com/devchan97/code-map/releases)
에서 OS 에 맞는 아카이브를 받아 압축을 풀고, 한 번만 `install-self`
를 실행합니다.

```sh
# Linux / macOS
./codemap install-self

# Windows (PowerShell, 압축을 푼 폴더에서)
.\codemap.exe install-self
```

바이너리가 `~/.codemap/bin` 에 복사되고 사용자 PATH 에 등록됩니다
(Windows 는 HKCU\Environment, Unix 는 `~/.bashrc` /
`~/.zshrc` / `~/.config/fish/config.fish` / `~/.profile` 의 마커
블록). 새 셸을 열면 어디서든 `codemap` 으로 호출됩니다. 되돌리려면
`codemap uninstall-self` 를 실행하세요.

Go 툴체인과 C 컴파일러가 이미 있으면
`go install github.com/devchan97/code-map/cmd/codemap@latest` 도
가능합니다 — `$GOPATH/bin` 에 설치되며, 보통 이미 PATH 에 잡혀
있습니다.

## 빠른 시작

```sh
# 1. 레포 초기 색인 (idempotent — 다시 실행해도 비용 없음).
codemap init /path/to/myrepo

# 2. 인크리멘털 리프레시 (변경된 파일만 다시 파싱).
codemap index .

# 3. 이 머신에 등록된 모든 인덱스 보기.
codemap list

# 4. 검색.
codemap search "applyRateLimit" --json

# 5. qualname 또는 file:line 으로 단일 심볼 조회.
codemap show api.middleware.apply_rate_limit
codemap show src/api/middleware.py:150

# 6. 그래프 질의.
codemap refs  api.middleware.apply_rate_limit   # 들어오는 참조 / caller
codemap calls api.middleware.apply_rate_limit   # 나가는 호출 / 참조

# 7. 인터랙티브 그래프 렌더.
codemap visualize . --open

# 8. 코딩 에이전트가 자동 사용하도록 SKILL.md 설치.
codemap install-skill --agent claude-code --scope user
```

모든 명령은 `--json` 을 지원합니다. JSON 스키마는 안정 계약이며 자세한 내용은
`architecture.md` §6.2 를 참고하세요.

## CLI 레퍼런스

| 명령 | 설명 |
| --- | --- |
| `codemap init [PATH]` | Idempotent 초기화. `.codemap/` 생성, 글로벌 레지스트리 등록, 첫 인크리멘털 인덱싱 실행. |
| `codemap index [PATH]` | 인크리멘털 색인. 기존 DB 재사용, SHA-1 이 바뀐 파일만 재파싱. |
| `codemap reindex [PATH]` | `symbols` / `edges` / `tokens` 를 drop 후 재구축. 스키마가 바뀌었을 때 사용. |
| `codemap list [--json]` | 등록된 모든 레포 표시 (NAME, PATH, LAST_INDEXED, FILES, SYMBOLS). |
| `codemap status [PATH\|NAME]` | 통계 + `lastIndexed`. |
| `codemap forget [PATH\|NAME]` | 레지스트리 항목 제거 (`.codemap/` 디렉토리는 그대로 유지). |
| `codemap search <QUERY> [flags]` | 주요 검색. 플래그: `--repo`, `--top`, `--kind`, `--scope`, `--file`, `--rerank`, `--json`. |
| `codemap show <QUALNAME\|FILE:LINE>` | 정의 + 스니펫. |
| `codemap refs <QUALNAME>` | 들어오는 참조 / caller 목록. |
| `codemap calls <QUALNAME>` | 나가는 호출 / 참조 목록. |
| `codemap visualize [PATH\|NAME] [flags]` | `graph.html` 렌더. 플래그: `--out`, `--open`. |
| `codemap install-skill` | SKILL.md 설치. 플래그: `--agent`, `--scope`, `--print`. |
| `codemap uninstall-skill` | 설치된 SKILL.md 제거. |
| `codemap install-self` | 실행 중인 바이너리를 `~/.codemap/bin` 에 복사하고 사용자 PATH 에 등록. |
| `codemap uninstall-self` | 설치된 바이너리 제거 + PATH 변경 되돌리기. |
| `codemap version` | 버전, 스키마 버전, 활성 embedder 출력. |

## 아키텍처

```
┌──────────────────────────┐
│ 코딩 에이전트 (Claude /  │
│ Codex) — 모든 추론은 여기 │
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
                                 └─ encoder rerank (옵션, build-tag)
```

- CLI 프로세스는 단발성입니다. 매 호출마다 SQLite 파일을 새로 엽니다.
  콜드 스타트 예산: ≤ 50 ms.
- per-repo 인덱스는 `<repo>/.codemap/index.db` 에 위치 (gitignore 권장).
- 글로벌 레지스트리는 `~/.codemap/registry.toml`. "이 `codemap search` 는 어느
  DB 를 쓰지?" 질문에는 6 단계 리졸버가 답합니다 (`architecture.md` §3.7).
- 파서 / 엔코더 / 인덱서는 분리되어 있습니다. 언어 어댑터는 `core.Symbol` /
  `core.Edge` 만 emit 하고, 영속화는 store 레이어의 책임입니다.

설계 의사결정 전체는 [`codemap-design.md`](./codemap-design.md), 모듈 단위
아키텍처는 [`architecture.md`](./architecture.md)를 보세요.

## SKILL.md 통합

`codemap install-skill` 은 Claude Code (Codex 는 스펙 확정 후) 가 파일을
통째로 읽기 전에 codemap 을 먼저 호출하도록 안내하는 SKILL.md 를 설치합니다.
원본 템플릿은 [`skill/SKILL.md.tmpl`](./skill/SKILL.md.tmpl) 에 있고, 설치
시점에 바이너리 이름과 버전이 치환됩니다.

기본 설치 경로:

- Claude Code (user 스코프): `~/.claude/skills/codemap/SKILL.md`
- Claude Code (project 스코프): `<repo>/.claude/skills/codemap/SKILL.md`
- Codex: 스펙 미확정 (릴리스 시점에 확인)

## 레포지토리 레이아웃

```
cmd/codemap/                 # CLI 엔트리포인트 (package main)
internal/
  cli/                       # cobra 서브커맨드 핸들러
  core/                      # 도메인 타입 + sentinel error
  walker/                    # 파일 시스템 순회 + ignore + secrets
  parser/                    # tree-sitter 어댑터 레지스트리
    python/                  # Python 어댑터 (CGO)
    java/                    # Java 어댑터 (CGO)
    ts/                      # JavaScript / TypeScript / TSX 어댑터 (CGO)
    csharp/                  # C# 어댑터 (CGO)
    cpp/                     # C++ 어댑터 (CGO)
    fallback/                # 미지원 언어용 whole-file 폴백
    {golang,rust}/           # 스캐폴딩 (후순위)
  lexical/                   # 토큰화 + BM25 + TokenStore 인터페이스
  encoder/                   # 옵션 dense rerank (build tag: encoder)
  store/                     # SQLite 게이트웨이 (modernc.org/sqlite, pure Go)
  registry/                  # ~/.codemap/registry.toml + 6 단계 리졸버
  pipeline/                  # init / index / reindex 오케스트레이션
  search/                    # search.Run + Show
  graph/                     # refs + calls
  visualize/                 # graph.html 렌더
  skill/                     # SKILL.md 설치기
  install/                   # codemap install-self / uninstall-self
  platform/                  # OS 추상화 (path, atomic write, browser)
skill/SKILL.md.tmpl          # SKILL.md 템플릿 원본
scripts/                     # zig-cc wrapper (릴리스 타깃별)
.github/workflows/           # ci.yml, release.yml
.goreleaser.yml              # 크로스 플랫폼 릴리스 설정 (zig-cc + CGO)
```

## 프라이버시

- **기본 로컬 only.** CLI 는 외부 네트워크 호출을 하지 않습니다. 인덱스,
  레지스트리, 렌더된 HTML 모두 머신을 떠나지 않습니다.
- **Secret 회피 walker.** 흔한 시크릿 패턴 (`.env`, `*.pem`, `*.key`,
  `id_rsa*`, `id_ed25519*`, ...) 에 매칭되는 파일은 본문 읽기 전에 스킵됩니다.
  내용 스캔에서 AWS 액세스 키 prefix 나 PEM 헤더가 있으면 거부됩니다.
  추가 경로를 제외하거나 프로젝트별로 기본값을 덮어쓰려면 레포 루트에
  `.codemapignore` 파일 (gitignore 문법) 을 두세요. `.gitignore` 와 함께
  매 walk 마다 적용됩니다.
- **텔레메트리 없음.** codemap 은 외부로 데이터를 보내지 않습니다.

## 개발

```sh
go test ./...        # CGO 무관 패키지 전체 테스트. python 어댑터 테스트는 CGO 활성 CI 에서만 실행.
go vet  ./...
go build ./...
go build -tags encoder ./...
```

CI (`.github/workflows/ci.yml`) 는 Linux / macOS / Windows 매트릭스로 돕니다.
tree-sitter 는 CGO 가 필요하기 때문에 Python 파서 테스트는 `//go:build cgo`
태그 뒤에 게이팅되어 있습니다. CI 러너에는 C 툴체인이 사전 설치되어 있고,
CGO 없는 개발 환경에서도 그 외 모든 코드와 테스트는 정상적으로 빌드/통과합니다.

릴리스는 `.github/workflows/release.yml` 이 담당합니다. `ubuntu-latest`
단일 러너에서 GoReleaser 를 돌리고, 모든 CGO 타깃은 zig-cc 를 범용 크로스
컴파일러로 사용합니다. 각 타깃별 `-target <triple>` 플래그는 `scripts/`
의 한 줄짜리 wrapper 스크립트에 박혀 있어, `$CC` / `$CXX` 를 단일 실행파일
경로로 유지합니다 — wrapper 없이 `CC=zig cc -target …` 처럼 공백을 포함하면
Go 링커 단계가 깨집니다. `v*` 태그 푸시마다 트리거됩니다. 현재는 linux
amd64 / arm64 와 windows amd64 가 게시되며, darwin 과 windows arm64 는
후순위입니다.

## 라이선스

[MIT](./LICENSE)
