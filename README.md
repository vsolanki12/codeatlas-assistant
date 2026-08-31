# CodeAtlas Assistant

A CLI tool that lets you talk to your codebase in plain English using local LLMs.

## What Is This?

[CodeAtlas](https://github.com/vsolanki12/codeatlas) builds a knowledge graph of your codebase — controllers, CRDs, functions, packages, and how they connect. It exposes this through CLI commands like `atlas search`, `atlas explain`, `atlas impact`, etc.

**CodeAtlas Assistant** sits on top of that. You ask a question in natural language, and it:

1. **Detects your intent** — are you asking how something works? what would break if you changed it? looking for a function?
2. **Runs bounded, structured Atlas queries** — primarily one compact compound JSON query plus only the follow-up data needed for the intent
3. **Feeds the graph evidence to a local Ollama model** — your question + compact Atlas data as context
4. **Streams the answer** — no cloud APIs, everything runs locally

It also has specialized modes for **analyzing JIRA issues** (paste a bug description, get root cause analysis with actual file paths), **generating Go code** that matches your existing codebase patterns, and running a bounded supplemental PR review.

### Why Not Just Use ChatGPT/Claude?

- **Runs 100% locally** — no code leaves your machine. Uses Ollama with any model you have.
- **Grounded in real architecture** — answers start from the extracted graph, not training-data guesses. Relationships carry evidence and confidence; ambiguous or unavailable facts are reported instead of silently promoted to facts.
- **Codebase-aware code generation** — with `--repo`, the generate mode reads only graph-selected source spans/files and instructs the model to match established patterns (import aliases, error handling, logging style, naming conventions).
- **Works offline** — airport, VPN issues, air-gapped environments.

### Grounding and freshness

Atlas is the repository-facts layer; the local model is a reasoning layer. The
assistant prefers compact `--json` Atlas responses, carries relationship
evidence and graph status into prompts, and tells the model that inferred,
heuristic, truncated, or unavailable data is not proof. Search and read-only
questions can report partial graphs, but solve, generate, and Claude prompt
generation require a current, complete, verifiable graph when a repository is
provided. When `--repo` is supplied, generated file and function references
are checked against the graph before they are accepted.
For read-only questions, any incomplete or unverified graph status is included
in the model prompt so the answer reports that limit instead of hiding it in
the CLI warning stream.

The embedded conventions are repository-neutral. Supply `--conventions` for
project-specific rules; the assistant does not assume HyperShift, Kubernetes,
or a particular framework from its defaults.

Freshness context also carries CodeAtlas scan coverage. Failed parser files
block implementation workflows; ignored file types are reported as outside
the graph rather than treated as absent repository entities.

For a preflight check in automation, run CodeAtlas's strict verification command
against the same checkout and graph:

```bash
atlas verify --graph graph.json --repo ~/your-repo
```

The assistant also rejects missing or non-current CodeAtlas schema metadata for
implementation and review workflows. Read-only questions can still expose the
status in their prompt so the model reports uncertainty instead of treating a
partial graph as complete.

This keeps the assistant from becoming a second repository-analysis engine:
Atlas extracts and retrieves facts; the model explains them, identifies
uncertainty, and proposes engineering reasoning for a human to review.

## Prerequisites

- [CodeAtlas](https://github.com/vsolanki12/codeatlas) CLI installed (`go install github.com/vsolanki12/codeatlas/cmd/atlas@latest`); keep `atlas` on `PATH` or pass `--atlas-bin /path/to/atlas` (the `CODEATLAS_BIN` environment variable is also supported)
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

### Context Benchmark (no LLM)

Measure the context cost of a graph query before sending it to a model:

```bash
assistant --graph graph.json --benchmark controller:example.com/repo/pkg.Reconciler
assistant --graph graph.json --repo ~/your-repo \
  --benchmark controller:example.com/repo/pkg.Reconciler --benchmark-json
```

The benchmark performs one full and one compact deterministic Atlas query. With
`--repo`, it also measures source files explicitly named by the graph; it does
not walk or rescan the repository and does not invoke Ollama. It reports prompt
size, approximate token reduction, selected files, and truncation status. The
approximation is for comparison only; Ollama emits actual token counts during
generation.

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

For implementation-oriented modes, pass `--repo` pointing to the same checkout
used to build the graph. The Assistant then reads only the files and source
spans selected by CodeAtlas; it does not walk the repository or perform a
second architecture scan. If CodeAtlas cannot identify exactly one safe
implementation controller when controller candidates are present, solve and
Claude implementation workflows stop and ask for a narrower question or exact
entity ID instead of choosing by rank. Function-oriented Go repositories with
no controller entities can still use graph-selected functions and files.

### Supplemental PR Review Mode

Review a bounded local packet containing a PR dossier, diff, repository guidance,
Atlas evidence, and test targets:

```bash
assistant --review-file review-packet.md \
  --graph ~/codeatlas/hypershift-graph.json \
  --model qwen3.8:27b \
  --num-ctx 24576 --max-output 1800
```

The output separates supported changed-line findings, architecture and impact,
verification additions, and unknowns. The packet is treated as untrusted data;
the assistant does not execute commands from it or modify the repository. The
Python PR reviewer invokes this mode automatically when it finds
`~/codeatlas-assistant/assistant`.

For a local diff, `--review-diff` asks CodeAtlas to map changed lines to graph
entities and relationships before the local model reasons about defects. The
packet also carries a bounded `diffExcerpt` with changed source text, so exact
line findings have actual diff evidence:

```bash
assistant --review-diff pr.diff --review-base origin/main --repo ~/your-repo \
  --graph ~/codeatlas/graph.json
```

Verified review requires a current, complete graph and a matching checkout.
Structural test links remain evidence only; this workflow does not prove
branch-level or runtime coverage. If the diff excerpt is truncated, omitted
changed lines remain unknown. Graph freshness does not by itself prove that a
supplied diff file matches the named base/head refs.

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
                        │  Atlas CLI       │  atlas ask HostedCluster --json
                        │  (JSON contract) │  --graph graph.json
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
  benchmark/benchmark.go        — no-LLM full/compact/raw context measurements
  metrics/metrics.go            — byte/character/estimated-token measurements
  ollama/ollama.go              — LLM interface + Client for Ollama API
  intent/intent.go              — Intent detection, entity/term extraction
  prompt/prompt.go              — Template-based prompt builders
  prompt/templates/*.tmpl       — Prompt templates (ask, solve, generate, claude, review)
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
| `--repo` | empty | Same source checkout used to build the graph; enables freshness checks and graph-selected source snippets |
| `--model` | auto-detect | Ollama model name |
| `--num-ctx` | `24576` | Ollama context size |
| `--max-output` | `1800` | Maximum generated tokens per pass |
| `--interactive` | `false` | Enter REPL mode |
| `--solve` | | JIRA description text to analyze |
| `--solve-file` | | Path to file with JIRA description |
| `--claude` | | JIRA text — generate Claude-optimized prompt |
| `--claude-file` | | Path to file — generate Claude-optimized prompt |
| `--generate` | | Description of Go code to generate |
| `--benchmark` | | Exact CodeAtlas entity to benchmark; performs no LLM generation |
| `--benchmark-json` | `false` | Emit the benchmark result as machine-readable JSON |
| `--review-file` | | Read a local PR review packet and run supplemental review |
| `--review-diff` | | Build a deterministic CodeAtlas review packet from a diff file or `-` |
| `--review-base` | | Base Git ref for `--review-diff` |
| `--review-head` | `HEAD` | Head Git ref for `--review-diff` |
| `--style-file` | auto-detect | Go file to use as style reference |
| `--conventions` | generic embedded defaults | Custom project conventions file |
| `--force-solve` | `false` | Skip existing fix check in solve mode |

## License

MIT
