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
	"github.com/vsolanki12/codeatlas-assistant/internal/validate"
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

func Run(a atlas.Runner, llm ollama.LLM, jiraText, conventions, outputFile, repoPath string, distillOnly bool) {
	if f := atlas.CheckFreshness(a, repoPath); f.BlocksImplementation() {
		fmt.Fprintf(os.Stderr, "--- ERROR: %s ---\n", f.Warning())
		return
	} else if warning := f.Warning(); warning != "" {
		fmt.Fprintf(os.Stderr, "--- WARNING: %s ---\n", warning)
	}

	result := gather.FromJIRA(a, jiraText)

	atlasData := result.AtlasData
	if atlasData == "" {
		fmt.Fprintln(os.Stderr, "atlas error: no CodeAtlas evidence matched this JIRA; refusing to generate a Claude implementation prompt")
		return
	}

	atlasData = result.Packet.PromptEvidence()

	entries := toEntries(result.Controllers)
	ws, err := workingset.FromEvidence(repoPath, result.Packet, atlas.SourceBudget(a))
	if err != nil || len(ws.ImplFiles) == 0 {
		fmt.Fprintf(os.Stderr, "atlas error: implementation evidence is unavailable: %v\n", err)
		return
	}
	workload := ""
	if len(result.Controllers) == 1 {
		workload = result.Controllers[0].ID
	}
	var framework *prompt.FrameworkInfo
	claudeTemplate := prompt.BuildClaude(jiraText, atlasData+"\n\n"+ws.PromptContext(), conventions, "", "", "", framework, entries)
	fmt.Fprintln(os.Stderr, "--- Distilling Claude prompt ---")
	distilled, err := llm.GenerateString(claudeTemplate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error distilling claude prompt: %v\n", err)
		return
	}
	vr := validate.ClaudeXML(distilled, repoPath, a)
	if !vr.OK() {
		fmt.Fprintf(os.Stderr, "--- VALIDATION ERROR ---\n%s", vr.Report())
		return
	}
	if err := os.WriteFile(outputFile, []byte(distilled), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing claude prompt: %v\n", err)
		return
	}
	manifest := buildManifest(workload, ws, framework, ws.TypeFiles)
	manifestPath := strings.TrimSuffix(outputFile, filepath.Ext(outputFile)) + "-manifest.json"
	writeManifest(manifestPath, manifest)
	if distillOnly {
		return
	}
	p := prompt.BuildWorkingSetSolve(jiraText, conventions, workload, ws.Functions, toPromptFiles(ws.ImplFiles), toPromptFiles(ws.TestFiles), ws.Types)
	p += "\n\n## Atlas selection evidence\n" + atlasData
	for _, omission := range ws.Omissions {
		p += "\nSource limitation: " + omission
	}

	output, err := llm.GenerateString(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		return
	}
	if vr := validate.Output(output, repoPath, a); !vr.OK() {
		fmt.Fprintf(os.Stderr, "--- VALIDATION ERROR ---\n%s", vr.Report())
		return
	}
	fmt.Print(output)
}

func toPromptFiles(files []workingset.FileContent) []prompt.FileContent {
	out := make([]prompt.FileContent, len(files))
	for i, f := range files {
		out[i] = prompt.FileContent{Path: f.Path, Code: f.Code, Evidence: f.Evidence}
	}
	return out
}

func toEntries(controllers []gather.ControllerInfo) []prompt.ControllerEntry {
	entries := make([]prompt.ControllerEntry, len(controllers))
	for i, c := range controllers {
		entries[i] = prompt.ControllerEntry{ID: c.ID, File: c.File, Role: c.Role}
	}
	return entries
}

func DefaultOutputName(inputFile string) string {
	base := filepath.Base(inputFile)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return name + "-claude.xml"
}
