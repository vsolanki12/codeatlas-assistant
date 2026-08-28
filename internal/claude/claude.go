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

	entries := toEntries(result.Controllers)

	// CodeAtlas is the authority for repository structure. The Assistant may
	// read files selected by graph entities later, but it does not independently
	// walk the repository or infer a framework/component topology here.
	var framework *prompt.FrameworkInfo
	repoFiles := ""

	selectedController, uniqueController := gather.SelectImplementationController(result.Controllers)
	workload := ""
	workloadFile := ""
	if uniqueController {
		workload = selectedController.ID
		workloadFile = selectedController.File
	}
	focusedData := atlasData
	if workload != "" {
		fmt.Fprintf(os.Stderr, "--- Workload controller: %s ---\n", workload)
		focused := gatherControllerData(a, workload)
		if focused != "" {
			focusedData = focused
		}
	}
	if repoPath != "" && !uniqueController {
		fmt.Fprintln(os.Stderr, "atlas error: CodeAtlas did not identify exactly one implementation controller; provide a more specific request or exact CodeAtlas entity ID")
		return
	}

	apiTypes := ""
	var apiTypeFiles []string

	claudeTemplate := prompt.BuildClaude(jiraText, focusedData, conventions, result.StyleCode, repoFiles, apiTypes, framework, entries)
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
	} else if vr.Checked > 0 {
		fmt.Fprintf(os.Stderr, "--- %s ---\n", vr.Report())
	}

	if err := os.WriteFile(outputFile, []byte(distilled), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing claude prompt: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "--- Claude prompt saved: %s ---\n", outputFile)
	}

	if distillOnly && repoPath != "" {
		ws := workingset.BuildForController(repoPath, focusedData, apiTypes, workload, workloadFile, a)
		manifest := buildManifest(workload, ws, framework, apiTypeFiles)
		manifestPath := strings.TrimSuffix(outputFile, filepath.Ext(outputFile)) + "-manifest.json"
		writeManifest(manifestPath, manifest)
		return
	} else if distillOnly {
		return
	}

	fmt.Fprintln(os.Stderr, "--- Generating solution ---")

	var p string
	if repoPath != "" {
		ws := workingset.BuildForController(repoPath, focusedData, apiTypes, workload, workloadFile, a)
		fmt.Fprintf(os.Stderr, "--- Working set: %d files, %d chars ---\n",
			len(ws.ImplFiles)+len(ws.TestFiles), ws.TotalChars())
		if len(ws.ImplFiles) == 0 {
			fmt.Fprintln(os.Stderr, "atlas error: no graph-selected implementation source was available; refusing to generate a solution")
			return
		}

		implFiles := toPromptFiles(ws.ImplFiles)
		testFiles := toPromptFiles(ws.TestFiles)
		p = prompt.BuildWorkingSetSolve(jiraText, conventions, workload, ws.Functions, implFiles, testFiles, ws.Types)
	} else {
		p = prompt.BuildSolve(jiraText, atlasData, conventions, result.StyleCode, apiTypes, "")
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
		out[i] = prompt.FileContent{Path: f.Path, Code: f.Code}
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

func gatherControllerData(a atlas.Runner, controllerID string) string {
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("### Workload Controller: %s\n", controllerID))

	if jr, ok := a.(atlas.JSONRunner); ok {
		if result, err := jr.RunJSON("ask", controllerID, "--intent", "debug", "--compact"); err == nil {
			buf.WriteString(fmt.Sprintf("#### Ask (debug)\n%s\n", result))
		}
	} else {
		if result, err := a.Run("explain", controllerID); err == nil {
			buf.WriteString(fmt.Sprintf("#### Explain\n%s\n", result))
		}
		if result, err := a.Run("investigate", controllerID); err == nil {
			buf.WriteString(fmt.Sprintf("#### Investigate\n%s\n", result))
		}
		if result, err := a.Run("impact", controllerID); err == nil {
			buf.WriteString(fmt.Sprintf("#### Impact\n%s\n", result))
		}
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
