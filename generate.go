package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var filePathPattern = regexp.MustCompile(`[a-zA-Z0-9_/.-]+\.go:\d+`)

func Generate(model, graphPath, description, styleFile string) {
	fmt.Fprintln(os.Stderr, "--- Extracting context ---")
	terms := ExtractTechnicalTerms(description)

	if len(terms) == 0 {
		terms = strings.Fields(description)
		if len(terms) > 5 {
			terms = terms[:5]
		}
	}

	if len(terms) > 6 {
		terms = terms[:6]
	}

	fmt.Fprintf(os.Stderr, "terms: %s\n", strings.Join(terms, ", "))

	fmt.Fprintln(os.Stderr, "--- Gathering atlas context ---")
	var atlasData strings.Builder

	for _, term := range terms {
		result, err := runAtlas(graphPath, "search", term)
		if err != nil || strings.Contains(result, "No matching") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Search: %s\n%s\n", term, result))
	}

	topTerm := terms[0]
	investigateResult, err := runAtlas(graphPath, "investigate", topTerm)
	if err == nil && !strings.Contains(investigateResult, "not found") {
		atlasData.WriteString(fmt.Sprintf("### Investigate: %s\n%s\n", topTerm, investigateResult))
	}

	explainResult, err := runAtlas(graphPath, "explain", topTerm)
	if err == nil && !strings.Contains(explainResult, "not found") {
		atlasData.WriteString(fmt.Sprintf("### Explain: %s\n%s\n", topTerm, explainResult))
	}

	styleCode := loadStyleReference(styleFile, atlasData.String(), graphPath)

	fmt.Fprintln(os.Stderr, "--- Generating code ---")
	prompt := BuildGeneratePrompt(description, atlasData.String(), styleCode)

	if err := AskOllamaStream(model, prompt); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
	}
}

func loadStyleReference(styleFile, atlasData, graphPath string) string {
	if styleFile != "" {
		return readStyleFile(styleFile)
	}

	repoRoot := detectRepoRoot(graphPath)
	if repoRoot == "" {
		return ""
	}

	goFile := extractGoFilePath(atlasData)
	if goFile == "" {
		return ""
	}

	fullPath := filepath.Join(repoRoot, goFile)
	return readStyleFile(fullPath)
}

func detectRepoRoot(graphPath string) string {
	f, err := os.Open(graphPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	header := string(buf[:n])

	for _, prefix := range []string{`"repository":"`, `"repository": "`} {
		idx := strings.Index(header, prefix)
		if idx == -1 {
			continue
		}
		start := idx + len(prefix)
		end := strings.Index(header[start:], `"`)
		if end == -1 {
			continue
		}
		repoPath := header[start : start+end]
		if _, err := os.Stat(repoPath); err == nil {
			return repoPath
		}
	}
	return ""
}

func extractGoFilePath(atlasData string) string {
	matches := filePathPattern.FindAllString(atlasData, -1)
	for _, m := range matches {
		parts := strings.SplitN(m, ":", 2)
		path := parts[0]
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		if strings.Contains(path, "controller") || strings.Contains(path, "reconcil") {
			return path
		}
	}
	if len(matches) > 0 {
		return strings.SplitN(matches[0], ":", 2)[0]
	}
	return ""
}

func readStyleFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "style file: %v (skipping)\n", err)
		return ""
	}

	content := string(data)

	lines := strings.Split(content, "\n")
	if len(lines) > 150 {
		lines = lines[:150]
		content = strings.Join(lines, "\n") + "\n// ... (truncated at 150 lines)\n"
	}

	fmt.Fprintf(os.Stderr, "style reference: %s (%d lines)\n", path, len(lines))
	return content
}

func BuildGeneratePrompt(description, atlasData, styleCode string) string {
	var styleSection string
	if styleCode != "" {
		styleSection = fmt.Sprintf(`
## Style Reference (MATCH THIS EXACTLY)
The code below is from the same codebase. Match its patterns exactly:
- Same import style and aliases
- Same error handling patterns (wrap with fmt.Errorf, use apierrors, etc.)
- Same logging style (ctrl.LoggerFrom vs log.FromContext)
- Same function signature patterns (context first, pointer receivers, etc.)
- Same condition/status update patterns
- Same naming conventions (camelCase for unexported, PascalCase for exported)

`+"```go\n"+styleCode+"\n```\n")
	}

	return fmt.Sprintf(`You are a senior Go/Kubernetes/HyperShift engineer writing production code.

Generate Go code that:
1. Follows the EXACT patterns from the style reference below (if provided)
2. Uses correct package names, function signatures, and types from the atlas data
3. Matches controller-runtime conventions from the existing codebase
4. Includes proper error handling matching the existing style

Only output Go code with brief comments. No explanations outside code blocks.

## What to Generate
%s

## CodeAtlas Architecture Data (entities, relationships, file paths)
%s
%s
## Go Code
`, description, atlasData, styleSection)
}
