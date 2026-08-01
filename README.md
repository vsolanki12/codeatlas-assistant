# CodeAtlas Assistant

A CLI tool that lets you talk to your codebase in plain English using local LLMs.

## What Is This?

[CodeAtlas](https://github.com/vsolanki12/codeatlas) builds a knowledge graph of your codebase — controllers, CRDs, functions, packages, and how they connect. It exposes this through CLI commands like `atlas search`, `atlas explain`, `atlas impact`, etc.

**CodeAtlas Assistant** sits on top of that. You ask a question in natural language, and it:

1. **Detects your intent** — are you asking how something works? what would break if you changed it? looking for a function?
2. **Runs the right atlas commands** — search, explain, impact, investigate — to gather relevant architecture data
3. **Feeds everything to a local Ollama model** — your question + the atlas data as context
4. **Streams the answer** — no cloud APIs, everything runs locally

It also has specialized modes for **analyzing JIRA issues** (paste a bug description, get root cause analysis with actual file paths) and **generating Go code** that matches your existing codebase patterns.

### Why Not Just Use ChatGPT/Claude?

- **Runs 100% locally** — no code leaves your machine. Uses Ollama with any model you have.
- **Grounded in real architecture** — answers come from the actual code graph, not training data hallucinations. Every entity, relationship, and file path is real.
- **Codebase-aware code generation** — the generate mode reads actual source files from your repo and instructs the model to match exact patterns (import aliases, error handling, logging style, naming conventions).
- **Works offline** — airport, VPN issues, air-gapped environments.

## Prerequisites

- [CodeAtlas](https://github.com/vsolanki12/codeatlas) CLI installed (`go install github.com/vsolanki12/codeatlas/cmd/atlas@latest`)
- [Ollama](https://ollama.ai) running locally with at least one model (`ollama pull qwen3:8b`)
- A scanned atlas graph (`atlas scan --output graph.json ~/your-repo`)

## Install

```bash
go install github.com/vsolanki12/codeatlas-assistant@latest
```

Or build from source:

```bash
git clone https://github.com/vsolanki12/codeatlas-assistant.git
cd codeatlas-assistant
go build -o assistant ./cmd/assistant/
```

## Modes

### Question Mode

Ask natural language questions about your codebase:

```bash
assistant --graph graph.json "what reconciles HostedCluster"
assistant --graph graph.json "what breaks if I change reconcileEtcd"
assistant --graph graph.json "tell me everything about NodePoolReconciler"
```

Intent is detected from keywords (explain, impact, investigate, search, stats) and mapped to the right atlas command automatically.

### Solve Mode

Feed a JIRA description, get root cause analysis + fix approach with actual file paths:

```bash
assistant --graph graph.json --solve "NodePool stuck in Provisioning after etcd recovery"
assistant --graph graph.json --solve-file jira-description.txt
```

### Generate Mode

Generate Go code that matches existing codebase patterns:

```bash
assistant --graph graph.json --generate "add a validation function for NodePool release image"
```

Style matching reads a real Go file from the target repo and instructs the model to match its import aliases, error handling, logging, and naming conventions:

```bash
# Explicit style reference
assistant --graph graph.json \
  --style-file ~/hypershift/hypershift-operator/controllers/nodepool/nodepool_controller.go \
  --generate "add NodePool release image validation"

# Auto-detect from atlas output (picks a controller file from the scanned repo)
assistant --graph graph.json --generate "add etcd health check"
```

### Claude Mode

Generate a structured, Claude-optimized implementation prompt from a JIRA description. Uses local LLM to distill atlas data into XML-tagged sections that Claude can act on immediately — no architecture discovery needed.

```bash
assistant --graph graph.json --claude "NodePool stuck in Provisioning after etcd recovery"
assistant --graph graph.json --claude-file jira-description.txt
```

Output is a ready-to-paste Claude Code prompt with `<jira>`, `<architecture>`, `<files>`, `<functions>`, `<tests>`, `<constraints>`, and `<task>` sections. Zero Claude tokens consumed until you paste the output.

### Interactive REPL

```bash
assistant --graph graph.json --interactive
```

Supports `solve:`, `claude:`, and `gen:` prefixes inline:

```
> what reconciles HostedCluster
> solve: NodePool stuck after etcd recovery...
> claude: NodePool stuck after etcd recovery...
> gen: add etcd health check function
> exit
```

## How It Works

```
                        ┌─────────────────┐
  "what reconciles      │ Intent Detection │  explain / impact / investigate /
   HostedCluster?"  ──> │ (keyword match)  │  search / stats / ask
                        └────────┬────────┘
                                 │
                        ┌────────▼────────┐
                        │ Entity Extraction│  "HostedCluster"
                        │ (term parsing)   │
                        └────────┬────────┘
                                 │
                        ┌────────▼────────┐
                        │  Atlas CLI       │  atlas explain HostedCluster
                        │  (shells out)    │  --graph graph.json
                        └────────┬────────┘
                                 │
                        ┌────────▼────────┐
                        │ Prompt Builder   │  question + atlas data
                        │                  │  + style reference (generate mode)
                        └────────┬────────┘
                                 │
                        ┌────────▼────────┐
                        │ Ollama Streaming │  POST /api/generate
                        │ (NDJSON parse)   │  prints tokens as they arrive
                        └─────────────────┘
```

## Architecture

```
cmd/assistant/main.go           — CLI entry point, flag parsing, REPL
internal/
  atlas/atlas.go                — Runner interface + Client for atlas CLI
  ollama/ollama.go              — LLM interface + Client for Ollama API
  intent/intent.go              — Intent detection, entity/term extraction
  prompt/prompt.go              — Template-based prompt builders
  prompt/templates/*.tmpl       — Prompt templates (ask, solve, generate, claude)
  prompt/conventions.go         — Embedded engineering conventions
  gather/gather.go              — Shared atlas data gathering pipeline
  style/style.go                — Style reference loading, repo root detection
  solve/solve.go                — JIRA analysis with existing fix detection
  generate/generate.go          — Code generation with style matching
  claude/claude.go              — Claude-optimized prompt generation
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--graph` | `atlas.json` | Path to atlas graph JSON |
| `--model` | auto-detect | Ollama model name |
| `--interactive` | `false` | Enter REPL mode |
| `--solve` | | JIRA description text to analyze |
| `--solve-file` | | Path to file with JIRA description |
| `--claude` | | JIRA text — generate Claude-optimized prompt |
| `--claude-file` | | Path to file — generate Claude-optimized prompt |
| `--generate` | | Description of Go code to generate |
| `--style-file` | auto-detect | Go file to use as style reference |
| `--conventions` | embedded | Custom conventions file |
| `--force-solve` | `false` | Skip existing fix check in solve mode |

## License

MIT
