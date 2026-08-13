---
name: check-repos
description: Check freshness of codeatlas and codeatlas-assistant repos, binaries, and graphs
---

# Check Repos Freshness

Run the following bash commands and report results in a summary table.

**Path detection:** Find codeatlas-assistant repo from current directory (`git rev-parse --show-toplevel`). Find codeatlas as a sibling directory, or search `$HOME/codeatlas`, `$HOME/src/codeatlas`, `$HOME/projects/codeatlas`. Skip checks for any repo not found and note it in output.

## 1. Detect paths

```bash
# Try current dir first for assistant
CANDIDATE=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -n "$CANDIDATE" ] && [ "$(basename "$CANDIDATE")" = "codeatlas-assistant" ]; then
  ASSISTANT_ROOT="$CANDIDATE"
else
  ASSISTANT_ROOT=""
  for candidate in "$HOME/codeatlas-assistant" "$HOME/src/codeatlas-assistant" "$HOME/projects/codeatlas-assistant"; do
    if [ -d "$candidate/.git" ]; then
      ASSISTANT_ROOT="$candidate"
      break
    fi
  done
fi

ATLAS_ROOT=""
if [ -n "$ASSISTANT_ROOT" ]; then
  ASSISTANT_PARENT=$(dirname "$ASSISTANT_ROOT")
  for candidate in "$ASSISTANT_PARENT/codeatlas" "$HOME/codeatlas" "$HOME/src/codeatlas" "$HOME/projects/codeatlas"; do
    if [ -d "$candidate/.git" ]; then
      ATLAS_ROOT="$candidate"
      break
    fi
  done
fi
echo "atlas=${ATLAS_ROOT:-NOT FOUND} assistant=${ASSISTANT_ROOT:-NOT FOUND}"
```

## 2. Repo sync status

For each found repo, check ahead/behind and last commit:

```bash
if [ -n "$ATLAS_ROOT" ]; then
  git -C "$ATLAS_ROOT" status -sb
  git -C "$ATLAS_ROOT" log -1 --format="%cr  %s"
fi

if [ -n "$ASSISTANT_ROOT" ]; then
  git -C "$ASSISTANT_ROOT" status -sb
  git -C "$ASSISTANT_ROOT" log -1 --format="%cr  %s"
fi
```

## 3. Binary freshness

Compare binary mtime to latest commit timestamp. Use `stat -f %m` on macOS or `stat -c %Y` on Linux.

```bash
if [ -n "$ATLAS_ROOT" ]; then
  COMMIT_TS=$(git -C "$ATLAS_ROOT" log -1 --format=%ct)
  BINARY_TS=$(stat -f %m "$ATLAS_ROOT/atlas" 2>/dev/null || stat -c %Y "$ATLAS_ROOT/atlas" 2>/dev/null || echo 0)
  echo "atlas: commit=$COMMIT_TS binary=$BINARY_TS stale=$([ "$BINARY_TS" -lt "$COMMIT_TS" ] && echo YES || echo no)"
fi

if [ -n "$ASSISTANT_ROOT" ]; then
  COMMIT_TS=$(git -C "$ASSISTANT_ROOT" log -1 --format=%ct)
  BINARY_TS=$(stat -f %m "$ASSISTANT_ROOT/assistant" 2>/dev/null || stat -c %Y "$ASSISTANT_ROOT/assistant" 2>/dev/null || echo 0)
  echo "assistant: commit=$COMMIT_TS binary=$BINARY_TS stale=$([ "$BINARY_TS" -lt "$COMMIT_TS" ] && echo YES || echo no)"
fi
```

## 4. Graph freshness

Find all `*-graph.json` files in the codeatlas repo and check each against its source repo:

```bash
if [ -n "$ATLAS_ROOT" ]; then
  for graph in "$ATLAS_ROOT"/*-graph.json; do
    [ -f "$graph" ] || continue
    GRAPH_COMMIT=$(python3 -c "import json; d=json.load(open('$graph')); print(d.get('commit','unknown')[:7])")
    REPO_PATH=$(python3 -c "import json; d=json.load(open('$graph')); print(d.get('repository','unknown'))")
    REPO_HEAD=$(git -C "$REPO_PATH" rev-parse --short HEAD 2>/dev/null || echo "unknown")
    NAME=$(basename "$graph")
    echo "$NAME: graph=$GRAPH_COMMIT repo_head=$REPO_HEAD match=$([ "$GRAPH_COMMIT" = "$REPO_HEAD" ] && echo YES || echo no)"
  done
fi
```

## 5. Output format

Present results as a single table:

```
| Artifact              | Status       | Detail                          |
|-----------------------|--------------|---------------------------------|
| codeatlas repo        | ahead/behind | last commit: <age> <subject>    |
| codeatlas-assistant   | ahead/behind | last commit: <age> <subject>    |
| atlas binary          | OK / STALE   | binary vs commit timestamps     |
| assistant binary      | OK / STALE   | binary vs commit timestamps     |
| <name>-graph.json     | OK / STALE   | graph=<sha> vs HEAD=<sha>       |
```

If anything is STALE, add recommendation lines below the table with actual paths:
- Stale binary: `cd <path> && go build -o atlas ./cmd/atlas/`
- Stale graph: `cd <path> && ./atlas scan -repo <repo_path> -output <name>-graph.json`
