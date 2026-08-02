package workingset

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
)

type FileContent struct {
	Path string
	Code string
}

type WorkingSet struct {
	Controller string
	ImplFiles  []FileContent
	TestFiles  []FileContent
	Types      string
	Functions  []string
}

func Build(repoPath, atlasData, apiTypes, controllerFile string, framework *prompt.FrameworkInfo) *WorkingSet {
	ws := &WorkingSet{
		Types: apiTypes,
	}

	functions := extractFunctions(atlasData)
	ws.Functions = functions

	if framework != nil {
		ws.ImplFiles = loadComponentFiles(repoPath, framework)
	}

	if controllerFile != "" && len(ws.ImplFiles) == 0 {
		absPath := filepath.Join(repoPath, controllerFile)
		code := readFileCapped(absPath, 500)
		if code != "" {
			ws.ImplFiles = append(ws.ImplFiles, FileContent{Path: controllerFile, Code: code})
		}
	}

	if controllerFile != "" && framework != nil {
		absPath := filepath.Join(repoPath, controllerFile)
		content, err := os.ReadFile(absPath)
		if err == nil {
			src := string(content)
			for _, fn := range functions {
				block := extractFuncBlock(src, fn)
				if block != "" && len(block) < 3000 {
					ws.ImplFiles = append(ws.ImplFiles, FileContent{
						Path: controllerFile + " → " + fn + "()",
						Code: block,
					})
					break
				}
			}
		}
	}

	testDirs := make(map[string]bool)
	for _, f := range ws.ImplFiles {
		dir := filepath.Dir(f.Path)
		if !strings.Contains(dir, "→") {
			testDirs[dir] = true
		}
	}
	for dir := range testDirs {
		tests := findTestFiles(repoPath, dir)
		for _, t := range tests {
			code := readFileCapped(filepath.Join(repoPath, t), 200)
			if code != "" {
				ws.TestFiles = append(ws.TestFiles, FileContent{Path: t, Code: code})
			}
			if len(ws.TestFiles) >= 3 {
				break
			}
		}
	}

	return ws
}

func (ws *WorkingSet) TotalChars() int {
	total := len(ws.Types)
	for _, f := range ws.ImplFiles {
		total += len(f.Code)
	}
	for _, f := range ws.TestFiles {
		total += len(f.Code)
	}
	return total
}

func loadComponentFiles(repoPath string, framework *prompt.FrameworkInfo) []FileContent {
	var files []FileContent
	for _, line := range strings.Split(framework.Components, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 2 {
			continue
		}
		compName := strings.TrimSpace(strings.TrimSuffix(parts[0], "/"))
		workloadFile := ""
		switch strings.TrimSpace(parts[1]) {
		case "Deployment":
			workloadFile = "deployment.go"
		case "StatefulSet":
			workloadFile = "statefulset.go"
		default:
			continue
		}

		relPath := filepath.Join(framework.RelPath, compName, workloadFile)
		absPath := filepath.Join(repoPath, relPath)
		code := readFileCapped(absPath, 300)
		if code != "" {
			files = append(files, FileContent{Path: relPath, Code: code})
		}

		if len(files) >= 5 {
			break
		}
	}
	return files
}

var funcEntityPattern = regexp.MustCompile(`function:\w+\.(?:\w+\.)?(\w+)`)
var callsPattern = regexp.MustCompile(`(?:Calls|calls):\s*(.+)`)

func extractFunctions(atlasData string) []string {
	seen := make(map[string]bool)
	var funcs []string

	for _, match := range funcEntityPattern.FindAllStringSubmatch(atlasData, -1) {
		name := match[1]
		if !seen[name] && len(name) > 3 {
			seen[name] = true
			funcs = append(funcs, name)
		}
	}

	for _, match := range callsPattern.FindAllStringSubmatch(atlasData, -1) {
		for _, name := range strings.Split(match[1], ",") {
			name = strings.TrimSpace(name)
			if idx := strings.LastIndex(name, "."); idx != -1 {
				name = name[idx+1:]
			}
			if !seen[name] && len(name) > 3 {
				seen[name] = true
				funcs = append(funcs, name)
			}
		}
	}

	return funcs
}

func extractFuncBlock(content, funcName string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "func") {
			continue
		}
		if !strings.Contains(trimmed, funcName) {
			continue
		}
		if !strings.Contains(trimmed, funcName+"(") && !strings.Contains(trimmed, funcName+" (") {
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

func findTestFiles(repoPath, relDir string) []string {
	absDir := filepath.Join(repoPath, relDir)
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil
	}
	var tests []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
			tests = append(tests, filepath.Join(relDir, e.Name()))
		}
	}
	return tests
}

func readFileCapped(path string, maxLines int) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, fmt.Sprintf("// ... truncated at %d lines", maxLines))
	}
	return strings.Join(lines, "\n")
}

var skipDirs = map[string]bool{
	"vendor": true, ".git": true, "_output": true, "client": true,
	"hack": true, "bin": true, "node_modules": true,
}

func DetectFramework(repoRoot string) *prompt.FrameworkInfo {
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

func PromoteFrameworkController(entries []prompt.ControllerEntry, frameworkPath string) {
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
