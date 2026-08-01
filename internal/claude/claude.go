package claude

import (
	"fmt"
	"os"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/gather"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
)

func Run(a atlas.Runner, llm ollama.LLM, jiraText, conventions string) {
	result := gather.FromJIRA(a, jiraText)

	atlasData := result.AtlasData
	if atlasData == "" {
		atlasData = "(no atlas data available)"
	}

	fmt.Fprintln(os.Stderr, "--- Generating Claude prompt ---")
	p := prompt.BuildClaude(jiraText, atlasData, conventions, result.StyleCode)

	if err := llm.Generate(p); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
	}
}
