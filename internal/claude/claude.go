package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/gather"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
)

func Run(a atlas.Runner, llm ollama.LLM, jiraText, conventions, outputFile, repoPath string) {
	result := gather.FromJIRA(a, jiraText)

	atlasData := result.AtlasData
	if atlasData == "" {
		atlasData = "(no atlas data available)"
	}

	entries := toEntries(result.Controllers)

	repoFiles := ""
	var framework *prompt.FrameworkInfo
	if repoPath != "" {
		repoFiles = walkRepo(repoPath)
		if repoFiles != "" {
			fmt.Fprintf(os.Stderr, "--- Repo file tree loaded: %d files ---\n", strings.Count(repoFiles, "\n"))
		}
		framework = detectFramework(repoPath)
		if framework != nil {
			fmt.Fprintf(os.Stderr, "--- Framework detected: %s (%d components) ---\n", framework.RelPath, framework.Count)
			promoteFrameworkController(entries, framework.RelPath)
		}
	}

	workload := findWorkload(entries)
	focusedData := atlasData
	if workload != "" {
		fmt.Fprintf(os.Stderr, "--- Workload controller: %s ---\n", workload)
		focused := gatherControllerData(a, workload)
		if focused != "" {
			focusedData = focused
		}
	}

	apiTypes := ""
	if repoPath != "" {
		reconcilesCRD := parseReconciles(focusedData)
		apiTypes = gatherAPITypes(repoPath, result.Terms, reconcilesCRD)
		if apiTypes != "" {
			fmt.Fprintf(os.Stderr, "--- API types loaded: %d chars ---\n", len(apiTypes))
		}
	}

	claudeTemplate := prompt.BuildClaude(jiraText, focusedData, conventions, result.StyleCode, repoFiles, apiTypes, framework, entries)
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
	p := prompt.BuildSolve(jiraText, atlasData, conventions, result.StyleCode, apiTypes)
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

func detectFramework(repoRoot string) *prompt.FrameworkInfo {
	parentCounts := make(map[string][]string)

	filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == "component.go" {
			dir := filepath.Dir(path)
			parent := filepath.Dir(dir)
			rel, err := filepath.Rel(repoRoot, dir)
			if err != nil {
				return nil
			}
			parentRel, _ := filepath.Rel(repoRoot, parent)
			parentCounts[parentRel] = append(parentCounts[parentRel], rel)
		}
		return nil
	})

	var bestParent string
	var bestChildren []string
	for parent, children := range parentCounts {
		if len(children) > len(bestChildren) {
			bestParent = parent
			bestChildren = children
		}
	}

	if len(bestChildren) < 3 {
		return nil
	}

	var lines []string
	for _, compDir := range bestChildren {
		name := filepath.Base(compDir)
		absDir := filepath.Join(repoRoot, compDir)
		goFiles, err := os.ReadDir(absDir)
		if err != nil {
			continue
		}

		workloadType := ""
		var fileNames []string
		for _, f := range goFiles {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") {
				continue
			}
			if strings.HasSuffix(f.Name(), "_test.go") {
				continue
			}
			fileNames = append(fileNames, f.Name())
			switch f.Name() {
			case "deployment.go":
				workloadType = "Deployment"
			case "statefulset.go":
				workloadType = "StatefulSet"
			}
		}
		if len(fileNames) == 0 {
			continue
		}
		if workloadType == "" {
			workloadType = "Other"
		}
		lines = append(lines, fmt.Sprintf("%s/ | %s | %s",
			name, workloadType, strings.Join(fileNames, " ")))
	}

	return &prompt.FrameworkInfo{
		RelPath:    bestParent,
		Components: strings.Join(lines, "\n"),
		Count:      len(lines),
	}
}

func promoteFrameworkController(entries []prompt.ControllerEntry, frameworkPath string) {
	bestIdx := -1
	bestLen := 0
	for i, e := range entries {
		p := commonPathPrefix(e.File, frameworkPath)
		if len(p) > bestLen {
			bestLen = len(p)
			bestIdx = i
		}
	}
	if bestIdx > 0 && bestLen > 0 {
		entries[bestIdx].Role = "workload"
		promoted := entries[bestIdx]
		copy(entries[1:bestIdx+1], entries[0:bestIdx])
		entries[0] = promoted
	}
}

func commonPathPrefix(a, b string) string {
	ap := strings.Split(a, "/")
	bp := strings.Split(b, "/")
	var common []string
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] != bp[i] {
			break
		}
		common = append(common, ap[i])
	}
	return strings.Join(common, "/")
}

func DefaultOutputName(inputFile string) string {
	base := filepath.Base(inputFile)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return name + "-claude.xml"
}

func parseReconciles(data string) string {
	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Reconciles:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "Reconciles:"))
		}
	}
	return ""
}

func gatherAPITypes(repoPath string, terms []string, reconcilesCRD string) string {
	searchTerms := filterCamelCase(terms)
	if reconcilesCRD != "" {
		searchTerms = append(searchTerms, reconcilesCRD+"Spec")
	}
	if len(searchTerms) == 0 {
		return ""
	}

	var result strings.Builder
	seen := make(map[string]bool)

	for _, term := range searchTerms {
		files := findTypeFiles(repoPath, term)
		for _, rel := range files {
			key := rel + ":" + term
			if seen[key] {
				continue
			}
			seen[key] = true

			content, err := os.ReadFile(filepath.Join(repoPath, rel))
			if err != nil {
				continue
			}
			block := extractTypeBlock(string(content), term)
			if block != "" {
				result.WriteString(fmt.Sprintf("// from %s\n%s\n\n", rel, block))
			}
		}
	}

	if result.Len() > 8000 {
		return result.String()[:8000] + "\n// ... (truncated)\n"
	}
	return result.String()
}

func filterCamelCase(terms []string) []string {
	var result []string
	for _, t := range terms {
		if len(t) >= 4 && t[0] >= 'A' && t[0] <= 'Z' {
			result = append(result, t)
		}
	}
	return result
}

func findTypeFiles(repoPath, term string) []string {
	out, err := exec.Command("grep", "-rl", "type "+term+" ", "--include=*.go", repoPath).Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, "vendor/") || strings.Contains(line, "zz_generated") || strings.HasSuffix(line, "_test.go") {
			continue
		}
		rel, err := filepath.Rel(repoPath, line)
		if err != nil {
			continue
		}
		files = append(files, rel)
	}
	return files
}

func extractTypeBlock(content, typeName string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "type "+typeName+" ") {
			continue
		}

		start := i
		for start > 0 && strings.HasPrefix(strings.TrimSpace(lines[start-1]), "//") {
			start--
		}

		if !strings.Contains(trimmed, "{") {
			return strings.Join(lines[start:i+1], "\n")
		}

		depth := 0
		end := i
		for end < len(lines) {
			depth += strings.Count(lines[end], "{") - strings.Count(lines[end], "}")
			if depth <= 0 {
				break
			}
			end++
		}
		return strings.Join(lines[start:end+1], "\n")
	}
	return ""
}
