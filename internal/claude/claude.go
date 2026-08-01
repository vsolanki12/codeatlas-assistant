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

	entries := toEntries(result.Controllers)

	workload := findWorkload(entries)
	focusedData := atlasData
	if workload != "" {
		fmt.Fprintf(os.Stderr, "--- Workload controller: %s ---\n", workload)
		focused := gatherControllerData(a, workload)
		if focused != "" {
			focusedData = focused
		}
	}

	claudeTemplate := prompt.BuildClaude(jiraText, focusedData, conventions, result.StyleCode, entries)
	fmt.Fprintln(os.Stderr, "--- Distilling Claude prompt ---")
	distilled, err := llm.GenerateString(claudeTemplate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error distilling claude prompt: %v\n", err)
		return
	}
	if err := os.WriteFile(outputFile, []byte(distilled), 0644); err != nil {
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

func toEntries(controllers []gather.ControllerInfo) []prompt.ControllerEntry {
	entries := make([]prompt.ControllerEntry, len(controllers))
	for i, c := range controllers {
		entries[i] = prompt.ControllerEntry{ID: c.ID, File: c.File, Role: c.Role}
	}
	return entries
}

func findWorkload(entries []prompt.ControllerEntry) string {
	for _, e := range entries {
		if e.Role == "workload" {
			return e.ID
		}
	}
	return ""
}

func gatherControllerData(a atlas.Runner, controllerID string) string {
	name := controllerID
	if idx := strings.LastIndex(name, "."); idx != -1 {
		name = name[idx+1:]
	}
	name = strings.TrimPrefix(name, "controller:")

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("### Workload Controller: %s\n", controllerID))

	if result, err := a.Run("explain", name); err == nil {
		buf.WriteString(fmt.Sprintf("#### Explain\n%s\n", result))
	}
	if result, err := a.Run("investigate", name); err == nil {
		buf.WriteString(fmt.Sprintf("#### Investigate\n%s\n", result))
	}
	if result, err := a.Run("impact", name); err == nil {
		buf.WriteString(fmt.Sprintf("#### Impact\n%s\n", result))
	}

	if buf.Len() < 200 {
		return ""
	}
	return buf.String()
}

func DefaultOutputName(inputFile string) string {
	base := filepath.Base(inputFile)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return name + "-claude.xml"
}
