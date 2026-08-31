# Generic Engineering Conventions

These defaults are deliberately repository-neutral. CodeAtlas Assistant must
derive repository-specific conventions from the supplied CodeAtlas evidence,
selected source context, and an explicit `--conventions` file.

## Grounding

- Treat CodeAtlas entities and evidence-bearing relationships as the source of repository facts.
- Preserve the distinction between proven, inferred, heuristic, truncated, and unknown information.
- Do not invent files, functions, packages, types, tests, relationships, or project-specific rules.
- If the graph does not establish a required fact, report `NEED_MORE_CONTEXT` or state that it is not in the graph.

## Implementation

- Match patterns shown by graph-selected source files.
- Preserve existing error handling, logging, naming, API compatibility, and package boundaries.
- Make the smallest change supported by the available evidence.
- Do not introduce a framework, dependency, configuration mechanism, or directory that the evidence does not establish.

## Testing

- Reuse existing tests and test patterns shown by CodeAtlas.
- Treat a structural `tested_by` relationship as evidence of a graph link, not proof of runtime or branch coverage.
- State when a required test relationship or implementation pattern is not present in the graph.

## Review

- Report only failure scenarios supported by the diff and CodeAtlas evidence.
- Separate deterministic facts, repository patterns, heuristics, model reasoning, and unknowns.
- Keep the human reviewer responsible for final engineering decisions.
