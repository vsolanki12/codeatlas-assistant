package solve

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/gather"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
	"github.com/vsolanki12/codeatlas-assistant/internal/style"
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

var jiraIDPattern = regexp.MustCompile(`[A-Z]+-\d+`)

func Run(a atlas.Runner, llm ollama.LLM, jiraText, conventions string, forceSolve bool, repoPath string) {
	if !forceSolve {
		repoRoot := style.DetectRepoRoot(a.GraphPath())
		if repoRoot != "" && checkExistingFix(jiraText, repoRoot) {
			return
		}
	}

	result := gather.FromJIRA(a, jiraText)

	atlasData := result.AtlasData
	if atlasData == "" {
		atlasData = "(no atlas data available)"
	}

	fmt.Fprintln(os.Stderr, "--- Generating solution ---")

	var p string
	if repoPath != "" {
		entries := toEntries(result.Controllers)
		framework := workingset.DetectFramework(repoPath)
		if framework != nil {
			fmt.Fprintf(os.Stderr, "--- Framework detected: %s (%d components) ---\n", framework.RelPath, framework.Count)
			workingset.PromoteFrameworkController(entries, framework.RelPath)
		}

		workload := ""
		workloadFile := ""
		for _, e := range entries {
			if e.Role == "workload" {
				workload = e.ID
				workloadFile = e.File
				break
			}
		}

		ws := workingset.Build(repoPath, atlasData, "", workloadFile, jiraText, framework)
		fmt.Fprintf(os.Stderr, "--- Working set: %d files, %d chars ---\n",
			len(ws.ImplFiles)+len(ws.TestFiles), ws.TotalChars())

		implFiles := toPromptFiles(ws.ImplFiles)
		testFiles := toPromptFiles(ws.TestFiles)
		p = prompt.BuildWorkingSetSolve(jiraText, conventions, workload, ws.Functions, implFiles, testFiles, ws.Types)
	} else {
		p = prompt.BuildSolve(jiraText, atlasData, conventions, result.StyleCode, "", "")
	}

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

func toPromptFiles(files []workingset.FileContent) []prompt.FileContent {
	out := make([]prompt.FileContent, len(files))
	for i, f := range files {
		out[i] = prompt.FileContent{Path: f.Path, Code: f.Code}
	}
	return out
}

func checkExistingFix(jiraText, repoRoot string) bool {
	ids := jiraIDPattern.FindAllString(jiraText, -1)
	if len(ids) == 0 {
		return false
	}

	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true

		fmt.Fprintf(os.Stderr, "--- Checking git history for %s ---\n", id)

		cmd := exec.Command("git", "log", "--all", "--oneline", "--grep="+id)
		cmd.Dir = repoRoot
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			lines := strings.TrimSpace(string(out))
			fmt.Printf("## Existing fix found for %s\n\n", id)
			fmt.Printf("Git commits referencing this JIRA:\n```\n%s\n```\n\n", lines)

			cmd = exec.Command("gh", "pr", "list", "--repo=openshift/hypershift", "--search="+id, "--state=merged", "--limit=5", "--json=number,title,mergedAt,url")
			cmd.Dir = repoRoot
			prOut, prErr := cmd.Output()
			if prErr == nil && len(prOut) > 3 {
				fmt.Printf("Merged PRs:\n```\n%s\n```\n\n", strings.TrimSpace(string(prOut)))
			}

			fmt.Println("**This JIRA appears to have an existing fix. Review the commits/PRs above before generating a new solution.**")
			fmt.Println("Run with `--force-solve` to skip this check and generate a solution anyway.")
			return true
		}
	}

	return false
}
