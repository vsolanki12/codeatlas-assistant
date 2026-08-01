# CodeAtlas Assistant

Natural language interface for [CodeAtlas](https://github.com/vsolanki12/codeatlas) using local Ollama models.

Ask questions about your codebase in plain English. The assistant detects intent, runs the right atlas commands, and streams an LLM-powered answer.

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
go build -o assistant .
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

### Interactive REPL

```bash
assistant --graph graph.json --interactive
```

Supports `solve:` and `gen:` prefixes inline:

```
> what reconciles HostedCluster
> solve: NodePool stuck after etcd recovery...
> gen: add etcd health check function
> exit
```

## How It Works

```
Question
  -> Intent Detection (keyword matching)
  -> Entity Extraction (technical term parsing)
  -> Atlas CLI Commands (search, explain, impact, investigate, ask, view, stats)
  -> Prompt Construction (question + atlas data + optional style reference)
  -> Ollama Streaming (/api/generate, NDJSON)
  -> Printed Answer
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--graph` | `atlas.json` | Path to atlas graph JSON |
| `--model` | auto-detect | Ollama model name |
| `--interactive` | `false` | Enter REPL mode |
| `--solve` | | JIRA description text to analyze |
| `--solve-file` | | Path to file with JIRA description |
| `--generate` | | Description of Go code to generate |
| `--style-file` | auto-detect | Go file to use as style reference |

## License

MIT
