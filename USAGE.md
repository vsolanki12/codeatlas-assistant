# CodeAtlas Assistant — Usage Examples

## Build

```bash
cd ~/codeatlas-assistant
go build -o assistant .
```

Requires `atlas` on PATH: `cd ~/codeatlas && go install ./cmd/atlas`

## Single-Shot Queries

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

# Auto-detect — extracts a controller file path from atlas output, reads it from the repo
# Requires the atlas graph to contain real file paths (not test fixtures)
./assistant --graph ~/codeatlas/hypershift-graph.json \
  --generate "add etcd health check to HostedClusterReconciler"
```

Auto-detection reads the `repository` field from the atlas graph JSON to find the source repo,
then picks a controller `.go` file from atlas output. Works when the graph was built by scanning
the actual repository (not test data).

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
  prefix with 'gen:' to generate Go code
  graph: ~/codeatlas/hypershift-graph.json | model: deepseek-coder-v2:latest

> what reconciles HostedCluster
(streams answer)

> solve: NodePool stuck in Provisioning after etcd recovery...
(analyzes and streams solution)

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
| ask (default) | anything else — uses atlas ask with fuzzy entity matching |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--graph` | `atlas.json` | Path to atlas graph JSON |
| `--model` | auto-detect | Ollama model name |
| `--interactive` | false | Enter REPL mode |
| `--solve` | — | JIRA description text to analyze |
| `--solve-file` | — | Path to file containing JIRA description |
| `--generate` | — | Description of Go code to generate |
| `--style-file` | auto-detect | Go file to use as style reference |
