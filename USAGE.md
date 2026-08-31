# CodeAtlas Assistant — Usage Examples

## Build

```bash
cd ~/codeatlas-assistant
go build -o assistant ./cmd/assistant/
```

Requires `atlas` on PATH: `cd ~/codeatlas && go install ./cmd/atlas`. If multiple
Atlas versions are installed, use `--atlas-bin /absolute/path/to/atlas` or set
`CODEATLAS_BIN` so the assistant and graph schema stay in sync.

Before implementation or review, verify the graph against the checkout:

```bash
atlas verify --graph ~/codeatlas/hypershift-graph.json --repo ~/hypershift
```

This checks the current schema, repository identity, commit/file state, and
complete scan coverage. Ignored files are reported but are not fatal unless
`--fail-on-ignored` is supplied. High-degree entity relationships can be
continued through CodeAtlas's `relationship_offset`/`relationship_limit`
parameters when using the CLI or MCP directly.

## Single-Shot Queries

The assistant invokes `atlas ask/search/... --json --compact` for prompt
context. Run the same commands directly when you need to inspect the graph:

```bash
atlas ask HostedClusterReconciler --intent debug --json --compact --graph ~/codeatlas/hypershift-graph.json
atlas ask HostedClusterReconciler --intent debug --json --graph ~/codeatlas/hypershift-graph.json
```

The first form is bounded for token-sensitive consumers; the second preserves
the full machine-readable entity payload.

```bash
# Explain — how does something work
./assistant --graph ~/codeatlas/hypershift-graph.json "what reconciles HostedCluster"
./assistant --graph ~/codeatlas/hypershift-graph.json "explain NodePool"
./assistant --graph ~/codeatlas/hypershift-graph.json "how does reconcileEtcd work"

# Impact — what breaks if I change something
./assistant --graph ~/codeatlas/hypershift-graph.json "what breaks if I change reconcileEtcd"
./assistant --graph ~/codeatlas/hypershift-graph.json "impact of changing HostedClusterReconciler"

# Investigate — deep dive
./assistant --graph ~/codeatlas/hypershift-graph.json "tell me everything about NodePoolReconciler"
./assistant --graph ~/codeatlas/hypershift-graph.json "debug HostedControlPlane"

# Search — find entities
./assistant --graph ~/codeatlas/hypershift-graph.json "find reconcileEtcd"
./assistant --graph ~/codeatlas/hypershift-graph.json "search NodePool"

# Stats — graph overview
./assistant --graph ~/codeatlas/hypershift-graph.json "how many entities are there"
```

## Context Benchmark — No LLM

Compare the context sent to a model for full versus compact Atlas retrieval:

```bash
./assistant --graph ~/codeatlas/hypershift-graph.json \
  --benchmark controller:example.com/repo/pkg.Reconciler
```

Add `--repo` to measure the graph-selected source files that form the raw
repository baseline:

```bash
./assistant --graph ~/codeatlas/hypershift-graph.json --repo ~/hypershift \
  --benchmark controller:example.com/repo/pkg.Reconciler --benchmark-json
```

This runs two deterministic Atlas queries and no Ollama request. It never walks
the repository: source measurement uses only files named by returned CodeAtlas
entities. Token values are estimates (`~4` Unicode characters/token), while
normal Ollama requests report actual prompt/output counts on stderr.

## Solve Mode — JIRA Issue Analysis

Feed a JIRA description, get root cause analysis + fix approach + file paths.

```bash
# Inline
./assistant --graph ~/codeatlas/hypershift-graph.json \
  --solve "NodePool stuck in Provisioning state after etcd recovery. reconcileNodePool might miss the recovery completion signal."

# From file
./assistant --graph ~/codeatlas/hypershift-graph.json --solve-file jira-description.txt
```

Output includes:
- **Root Cause**: What's likely causing the issue based on code architecture
- **Files to Change**: Specific file paths from atlas data
- **Approach**: Step-by-step fix referencing actual functions and controllers
- **Tests**: Existing test coverage + suggested new tests

## Generate Mode — Go Code Generation

Generate Go code that follows existing codebase patterns.

```bash
./assistant --graph ~/codeatlas/hypershift-graph.json \
  --generate "add a validation function for NodePool that checks if the release image is valid"

./assistant --graph ~/codeatlas/hypershift-graph.json \
  --generate "write a reconciler for cleaning up orphaned HostedControlPlane resources"

./assistant --graph ~/codeatlas/hypershift-graph.json \
  --generate "add a new condition check in HostedClusterReconciler for etcd health"
```

### Style Matching with `--style-file`

Point at a real Go file from the target repo so generated code matches its patterns
(import aliases, error handling, logging, naming conventions):

```bash
# Explicit style file — best results
./assistant --graph ~/codeatlas/hypershift-graph.json \
  --style-file ~/hypershift/hypershift-operator/controllers/nodepool/nodepool_controller.go \
  --generate "add a function to validate NodePool release image before provisioning"

# Auto-detect — extracts a graph-selected controller path and reads that source file
# Requires a current graph built from the real checkout
./assistant --graph ~/codeatlas/hypershift-graph.json \
  --generate "add etcd health check to HostedClusterReconciler"
```

Auto-detection reads the `repository` field from the atlas graph JSON to find the source repo,
then uses a controller path already present in Atlas output. It reads only that selected file
and graph-selected function spans; it does not walk the repository or infer other files.

## Claude Mode — Generate Claude-Optimized Prompts

Generate a structured implementation prompt with XML tags that Claude can act on immediately.
No Claude tokens consumed until you paste the output. Everything runs locally via Ollama.

```bash
# From file (recommended)
./assistant --graph ~/codeatlas/hypershift-graph.json --solve-file jira.txt --claude

# With custom output name
./assistant --graph ~/codeatlas/hypershift-graph.json --solve-file jira.txt --claude --output my-prompt.xml

# From --claude-file (standalone)
./assistant --graph ~/codeatlas/hypershift-graph.json --claude-file jira-description.txt
```

**With `--repo`** (recommended): Uses the same checkout that produced the graph.
The assistant reads only graph-selected source files and function spans for the
working set; it does not scan the repository, discover API types, or build a
second architecture model. With `--repo`, implementation workflows also stop
when CodeAtlas identifies multiple possible implementation controllers; use a
narrower request or exact entity ID rather than relying on ranking. A generic
Go repository with no controller entities can use graph-selected functions
directly.

```bash
./assistant --graph ~/codeatlas/hypershift-graph.json --claude-file jira.txt --repo ~/hypershift
```

**Dual output:**
- **Screen** — human-readable engineering analysis (streamed via Ollama)
- **File** — distilled XML prompt for Claude (saved to `<input>-claude.xml`)

The assistant prefers one bounded structured Atlas query and caps raw Atlas context at roughly 24K characters before prompting the local model. With `--repo`, source snippets are selected by exact CodeAtlas entity IDs and source spans, then capped before prompting. For read-only questions, incomplete or unverified graph status is included in-band in the prompt; implementation guidance is refused for partial, stale, or unverifiable graphs. The exact token reduction depends on the graph and question.
XML sections include `<jira>`, `<architecture>`, `<files>`, `<functions>`, `<tests>`,
`<constraints>`, and `<task>`.

**Workflow:**
```
JIRA → bounded Atlas JSON → local LLM distills to XML → file saved
                                 → local LLM streams analysis → screen
                                                    ↓
                              paste XML into Claude Code → implementation
```

Zero Claude tokens until you paste. The compact evidence packet is intended to
reduce downstream context and token cost without treating model output as
repository fact.

## Supplemental PR Review Mode

The review mode is designed for packets produced by
`~/.claude/skills/codeatlas/scripts/review_pr.py`. It runs a separate local
review over the bounded diff and CodeAtlas evidence:

```bash
./assistant --review-file review-packet.md \
  --graph ~/codeatlas/hypershift-graph.json \
  --model qwen3.8:27b --num-ctx 24576 --max-output 1800
```

Output sections are:

- supported changed-line findings
- architecture and impact
- verification additions
- unknowns requiring human or live-cluster validation

The packet is treated as untrusted data. This mode does not execute packet
content, write source files, or post GitHub comments. Set `OLLAMA_HOST` when
Ollama is not listening on the default local endpoint.

For a local diff, let CodeAtlas build the packet from the checkout and verify
the graph before the model runs:

```bash
./assistant --review-diff pr.diff --review-base origin/main --repo ~/your-repo \
  --graph ~/codeatlas/graph.json
```

Without `--repo`, CodeAtlas cannot verify the diff against the checkout. The
review remains unverified and must not be treated as proof of coverage. The
packet includes a bounded `diffExcerpt`; if it is truncated, findings about
omitted changed lines are not supportable.

## Specify Model

```bash
./assistant --model qwen3:8b --graph ~/codeatlas/hypershift-graph.json "explain NodePool"
./assistant --model deepseek-coder-v2:latest --graph ~/codeatlas/hypershift-graph.json --solve "bug description here"
```

Auto-detects first available Ollama model if `--model` is omitted.

## Interactive REPL

```bash
./assistant --graph ~/codeatlas/hypershift-graph.json --interactive
```

```
CodeAtlas Assistant (type 'exit' to quit)
  prefix with 'solve:' to analyze a JIRA description
  prefix with 'claude:' to generate Claude prompt
  prefix with 'gen:' to generate Go code
  graph: ~/codeatlas/hypershift-graph.json | model: deepseek-coder-v2:latest

> what reconciles HostedCluster
(streams answer)

> solve: NodePool stuck in Provisioning after etcd recovery...
(analyzes and streams solution)

> claude: NodePool stuck in Provisioning after etcd recovery...
(generates Claude-ready prompt with XML tags)

> gen: add etcd health check function
(generates Go code)

> exit
```

## Intent Detection Keywords

| Intent | Trigger words |
|--------|--------------|
| explain | explain, how does, how do, work, reconcil, what does |
| impact | impact, break, affect, what breaks, blast radius, change |
| investigate | everything, investigate, debug, all about, tell me about, deep dive |
| search | search, find, where is, list, show me |
| stats | stats, statistics, count, how many, overview |
| ask (default) | anything else — uses Atlas ask with an exact entity ID or an unambiguous entity name |

## Conventions File

The tool embeds repository-neutral engineering conventions. This is injected
into solve, generate, Claude, and review prompts without claiming any
project-specific framework or directory structure.

Override with a project conventions file when the repository has local rules:

```bash
./assistant --conventions ~/my-project/conventions.md --graph graph.json --solve "bug description"
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--graph` | `atlas.json` | Path to atlas graph JSON |
| `--repo` | empty | Same source checkout used to build the graph; enables freshness checks and graph-selected source snippets |
| `--model` | auto-detect | Ollama model name |
| `--num-ctx` | `24576` | Ollama context size |
| `--max-output` | `1800` | Maximum generated tokens |
| `--interactive` | false | Enter REPL mode |
| `--solve` | — | JIRA description text to analyze |
| `--solve-file` | — | Path to file containing JIRA description |
| `--claude` | false | Save distilled XML prompt to file + stream analysis to screen |
| `--claude-file` | — | Path to file — generate Claude-optimized prompt |
| `--output` | auto | Output file for Claude prompt (default: `<input>-claude.xml`) |
| `--generate` | — | Description of Go code to generate |
| `--benchmark` | — | Exact CodeAtlas entity; compare full/compact context without an LLM |
| `--benchmark-json` | false | Emit the benchmark result as JSON; requires `--benchmark` |
| `--review-file` | — | Path to a local PR review packet |
| `--review-diff` | — | Build a deterministic CodeAtlas review packet from a diff file or `-` |
| `--review-base` | — | Base Git ref for `--review-diff` |
| `--review-head` | `HEAD` | Head Git ref for `--review-diff` |
| `--force-solve` | false | Skip existing fix check in solve mode |
| `--style-file` | auto-detect | Go file to use as style reference |
| `--conventions` | embedded | Conventions file for domain knowledge |
