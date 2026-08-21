# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository. `CLAUDE.md` is a symlink to this file so that Claude Code auto-loads it; the `AGENTS.md` name is canonical.

CodeAtlas Assistant is a local-first CLI that answers natural-language questions about a codebase by grounding answers in a [CodeAtlas](https://github.com/vsolanki12/codeatlas) knowledge graph and a local Ollama LLM. It supports question answering, JIRA root-cause analysis, style-matched Go code generation, and Claude prompt distillation — all running locally with zero cloud API calls.

This file is intentionally minimal — detailed guidance lives in the referenced files below and should be updated there, not here.

## Key References

| Topic | Where to look |
|-------|---------------|
| **Product overview and install** | [README.md](README.md) |
| **Detailed usage examples and flags** | [USAGE.md](USAGE.md) |
| **Domain conventions (injected into prompts)** | [conventions.md](conventions.md) |
| **Repo/binary/graph freshness checks** | [.claude/skills/check-repos/SKILL.md](.claude/skills/check-repos/SKILL.md) |

## Development Workflow

```bash
go build -o assistant ./cmd/assistant/   # Build
go test ./...                            # Run all tests
```

There is no `Makefile`, CI, or Docker configuration — the project builds with standard Go tooling. The module has **zero external Go dependencies** (stdlib only).

### Prerequisites

- [CodeAtlas](https://github.com/vsolanki12/codeatlas) CLI (`atlas`) on `PATH`
- [Ollama](https://ollama.ai) running locally with at least one model
- A pre-scanned Atlas graph (`atlas scan -repo /path/to/repo -output graph.json`)

## Code Architecture

```
cmd/assistant/main.go           — CLI entry point, flag parsing, model resolution, REPL
internal/
  atlas/                        — Runner interface + Client for shelling out to atlas CLI
  ollama/                       — LLM interface + Client for Ollama HTTP API (streaming)
  intent/                       — Intent detection + entity/term extraction from questions
  prompt/                       — Template-based prompt builders (embeds templates/*.tmpl)
  prompt/templates/             — Go templates for ask, solve, generate, claude modes
  gather/                       — Shared JIRA-to-atlas data gathering pipeline
  style/                        — Style-reference file loading + repo root detection
  solve/                        — JIRA root-cause analysis mode
  generate/                     — Go code generation with style matching
  claude/                       — Claude XML prompt + JSON manifest generation
  workingset/                   — Repo working-set assembly (impl/tests/types/functions)
  validate/                     — Validate LLM/XML path & entity references against repo/graph
```

### Key Interfaces

- **`atlas.Runner`** — `Run(...string) (string, error)` + `GraphPath() string`. Abstracts the atlas CLI; mockable in tests.
- **`ollama.LLM`** — `Generate(prompt)` + `GenerateString(prompt)`. Abstracts the Ollama HTTP API.

### Pipeline Flow

All modes follow the same pattern:

```
User Input → Intent Detection → Entity Extraction → Atlas CLI queries
  → Prompt Builder (template + conventions + atlas data) → Ollama streaming → Output
```

## CLI Modes

| Mode | Flag | Purpose |
|------|------|---------|
| Question | (positional args) | Natural-language questions routed by intent (explain, impact, investigate, search, stats) |
| Solve | `--solve` / `--solve-file` | JIRA description → root cause + files + approach + tests |
| Generate | `--generate` | Go code generation with style matching from `--style-file` or auto-detect |
| Claude | `--claude` / `--claude-file` | Distill atlas data into Claude-ready XML prompt (zero Claude tokens until paste) |
| Interactive | `--interactive` | REPL with `solve:`, `claude:`, `gen:` prefixes |

## Testing Conventions

- Tests are colocated with packages (`*_test.go` in the same directory)
- Standard Go `testing` package only — no external test frameworks
- Table-driven tests where appropriate
- Prompt "contract" tests lock critical instruction phrases in rendered templates
- Mock `atlas.Runner` for testing atlas-dependent code without a real binary
- Run all tests with `go test ./...`
- Untested packages to be aware of: `claude`, `gather`, `generate`, `solve`, `style`, `cmd/assistant`

## Conventions and Domain Knowledge

The `conventions.md` file contains HyperShift-specific engineering conventions (feature gates, API design, components, testing patterns, directory structure). This file is embedded into prompts for solve, generate, and claude modes. Override with `--conventions` for other projects.

## Companion Project

[CodeAtlas](https://github.com/vsolanki12/codeatlas) is the upstream dependency — it provides the `atlas` CLI and builds the graph that this assistant queries. Changes to atlas CLI output format, graph schema, or command flags may require updates here. The [check-repos skill](.claude/skills/check-repos/SKILL.md) can verify both repos are in sync.
