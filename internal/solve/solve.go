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
	"github.com/vsolanki12/codeatlas-assistant/internal/validate"
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

var jiraIDPattern = regexp.MustCompile(`[A-Z]+-\d+`)

func Run(a atlas.Runner, llm ollama.LLM, jiraText, conventions string, forceSolve bool, repoPath string) {
	if freshness := atlas.CheckFreshness(a, repoPath); freshness.BlocksImplementation() {
		fmt.Fprintf(os.Stderr, "atlas error: %s\n", freshness.Warning())
		return
	} else if warning := freshness.Warning(); warning != "" {
		fmt.Fprintf(os.Stderr, "atlas warning: %s\n", warning)
	}

	if !forceSolve {
		repoRoot := style.DetectRepoRoot(a.GraphPath())
		if repoRoot != "" && checkExistingFix(jiraText, repoRoot) {
			return
		}
	}

	result := gather.FromJIRA(a, jiraText)

	atlasData := result.AtlasData
	if atlasData == "" {
		fmt.Fprintln(os.Stderr, "atlas error: no CodeAtlas evidence matched this JIRA; refusing to generate implementation guidance")
		return
	}

	fmt.Fprintln(os.Stderr, "--- Generating solution ---")

	var p string
	if repoPath != "" {
		selected := gather.ControllerInfo{}
		if len(result.Controllers) > 0 {
			var unique bool
			selected, unique = gather.SelectImplementationController(result.Controllers)
			if !unique {
				fmt.Fprintln(os.Stderr, "atlas error: CodeAtlas identified multiple implementation controllers; provide a more specific request or exact CodeAtlas entity ID")
				return
			}
		}
		workload := selected.ID
		workloadFile := selected.File

		ws := workingset.BuildForController(repoPath, atlasData, "", workload, workloadFile, a)
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
		p = prompt.BuildSolve(jiraText, atlasData, conventions, result.StyleCode, "", "")
	}

	output, err := llm.GenerateString(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		return
	}
	validation := validate.Output(output, repoPath, a)
	if !validation.OK() {
		fmt.Fprintf(os.Stderr, "--- VALIDATION ERROR ---\n%s", validation.Report())
		return
	}
	fmt.Print(output)
	if validation.Checked > 0 {
		fmt.Fprintf(os.Stderr, "\n--- %s ---\n", validation.Report())
	}
}

func toPromptFiles(files []workingset.FileContent) []prompt.FileContent {
	out := make([]prompt.FileContent, len(files))
	for i, f := range files {
		out[i] = prompt.FileContent{Path: f.Path, Code: f.Code, Evidence: f.Evidence}
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

			// Run from the target checkout so gh resolves the repository from its
			// configured remote. The assistant must not assume a particular
			// organization or project when checking for an existing fix.
			cmd = exec.Command("gh", "pr", "list", "--search="+id, "--state=merged", "--limit=5", "--json=number,title,mergedAt,url")
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
