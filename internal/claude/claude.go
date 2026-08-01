package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/gather"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
)

func Run(a atlas.Runner, llm ollama.LLM, jiraText, conventions, outputFile string) {
	result := gather.FromJIRA(a, jiraText)

	atlasData := result.AtlasData
	if atlasData == "" {
		atlasData = "(no atlas data available)"
	}

	claudePrompt := prompt.BuildClaude(jiraText, atlasData, conventions, result.StyleCode)
	if err := os.WriteFile(outputFile, []byte(claudePrompt), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing claude prompt: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "--- Claude prompt saved: %s ---\n", outputFile)
	}

	fmt.Fprintln(os.Stderr, "--- Generating solution ---")
	p := prompt.BuildSolve(jiraText, atlasData, conventions, result.StyleCode)

	if err := llm.Generate(p); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
	}
}

func DefaultOutputName(inputFile string) string {
	base := filepath.Base(inputFile)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return name + "-claude.md"
}
