package solve

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/gather"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
	"github.com/vsolanki12/codeatlas-assistant/internal/style"
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

	repoFiles := ""
	if repoPath != "" {
		full := walkRepo(repoPath)
		repoFiles = filterRepoTree(full, atlasData)
		if repoFiles != "" {
			fmt.Fprintf(os.Stderr, "--- Repo file tree loaded: %d files (filtered from %d) ---\n",
				strings.Count(repoFiles, "\n"), strings.Count(full, "\n"))
		}
	}

	fmt.Fprintln(os.Stderr, "--- Generating solution ---")
	p := prompt.BuildSolve(jiraText, atlasData, conventions, result.StyleCode, "", repoFiles)

	if err := llm.Generate(p); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
	}
}

var skipDirs = map[string]bool{
	"vendor": true, ".git": true, "_output": true, "client": true,
	"hack": true, "bin": true, "node_modules": true,
}

func walkRepo(root string) string {
	var files []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		if strings.HasPrefix(info.Name(), "zz_generated") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return strings.Join(files, "\n")
}

var goPathPattern = regexp.MustCompile(`(?:^|[\s(])([a-zA-Z][\w/.-]*\.go)`)

func filterRepoTree(fullTree, atlasData string) string {
	dirs := make(map[string]bool)
	for _, match := range goPathPattern.FindAllStringSubmatch(atlasData, -1) {
		dir := filepath.Dir(match[1])
		for dir != "." && dir != "" {
			dirs[dir] = true
			dir = filepath.Dir(dir)
		}
	}

	if len(dirs) == 0 {
		return fullTree
	}

	var filtered []string
	for _, line := range strings.Split(fullTree, "\n") {
		if line == "" {
			continue
		}
		dir := filepath.Dir(line)
		if dirs[dir] {
			filtered = append(filtered, line)
		}
	}

	return strings.Join(filtered, "\n")
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
