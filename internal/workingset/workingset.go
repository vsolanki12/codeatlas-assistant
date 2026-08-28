package workingset

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
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

func Build(repoPath, atlasData, apiTypes, controllerFile string, a ...atlas.Runner) *WorkingSet {
	return build(repoPath, atlasData, apiTypes, "", controllerFile, a...)
}

// BuildForController is the strict routing variant used by implementation
// workflows after gather has selected one exact graph controller ID.
func BuildForController(repoPath, atlasData, apiTypes, controllerID, controllerFile string, a ...atlas.Runner) *WorkingSet {
	return build(repoPath, atlasData, apiTypes, controllerID, controllerFile, a...)
}

func build(repoPath, atlasData, apiTypes, selectedControllerID, controllerFile string, a ...atlas.Runner) *WorkingSet {
	ws := &WorkingSet{
		Types: apiTypes,
	}

	functions := extractFunctions(atlasData)
	functionIDs := extractFunctionIDs(atlasData)
	if selectedControllerID != "" {
		if selected := extractFunctionIDsForController(atlasData, selectedControllerID); len(selected) > 0 {
			functionIDs = selected
		} else {
			functionIDs = nil
		}
	}
	// Preserve repository-unique IDs in the prompt. Bare names are retained
	// only for compatibility with older text-only Atlas output.
	ws.Functions = append([]string(nil), functionIDs...)
	if len(ws.Functions) == 0 && selectedControllerID == "" {
		ws.Functions = functions
	}
	controllerID := selectedControllerID
	if controllerID == "" {
		controllerID = extractControllerIDForFile(atlasData, controllerFile)
	}
	if controllerFile == "" && controllerID != "" {
		controllerFile = extractControllerFileForID(atlasData, controllerID)
	}

	refs := atlas.EntityRefs(atlasData)
	relationships := atlas.RelationshipRefs(atlasData)
	appendGraphSelectedSources(ws, repoPath, refs, relationships, controllerFile, selectedControllerID)
	if len(ws.ImplFiles) == 0 && controllerFile != "" {
		// Compatibility path for older text-only Atlas output that has names but
		// no structured source spans. It remains limited to the selected file.
		appendLegacyFunctionSource(ws, repoPath, controllerFile, functions)
	}

	var runner atlas.Runner
	if len(a) > 0 {
		runner = a[0]
	}

	if runner != nil && controllerFile != "" && controllerID != "" {
		ws.TestFiles = findTestsFromGraph(runner, repoPath, controllerID, functionIDs, atlasData)
	}

	limitWorkingSet(ws, 24000)

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

var funcEntityPattern = regexp.MustCompile(`function:[a-zA-Z0-9._/@+\-]+`)
var callsPattern = regexp.MustCompile(`(?:Calls|calls):\s*(.+)`)

func extractFunctions(atlasData string) []string {
	seen := make(map[string]bool)
	var funcs []string

	// Prefer the structured graph result. The text patterns below are retained
	// for compatibility with older Atlas binaries that did not support JSON.
	for _, ref := range atlas.EntityRefs(atlasData) {
		if ref.Kind != "function" && !strings.HasPrefix(ref.ID, "function:") {
			continue
		}
		name := ref.Name
		if name == "" {
			name = ref.ID[strings.LastIndex(ref.ID, ".")+1:]
		}
		if !seen[name] && len(name) > 3 {
			seen[name] = true
			funcs = append(funcs, name)
		}
	}

	for _, match := range funcEntityPattern.FindAllString(atlasData, -1) {
		name := match[strings.LastIndex(match, ".")+1:]
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

func extractFunctionIDs(atlasData string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, ref := range atlas.EntityRefs(atlasData) {
		if ref.Kind == "function" || strings.HasPrefix(ref.ID, "function:") {
			if !seen[ref.ID] {
				seen[ref.ID] = true
				ids = append(ids, ref.ID)
			}
		}
	}
	for _, id := range funcEntityPattern.FindAllString(atlasData, -1) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func extractFunctionIDsForController(atlasData, controllerID string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, relationship := range atlas.RelationshipRefs(atlasData) {
		if relationship.From != controllerID || relationship.Type != "calls" || !strings.HasPrefix(relationship.To, "function:") {
			continue
		}
		if !seen[relationship.To] {
			seen[relationship.To] = true
			ids = append(ids, relationship.To)
		}
	}
	sort.Strings(ids)
	return ids
}

func extractControllerIDForFile(atlasData, controllerFile string) string {
	if controllerFile == "" {
		return ""
	}
	wanted := filepath.ToSlash(controllerFile)
	for _, ref := range atlas.EntityRefs(atlasData) {
		if ref.Kind != "controller" && !strings.HasPrefix(ref.ID, "controller:") {
			continue
		}
		if filepath.ToSlash(ref.Source.File) == wanted || containsFile(ref.Files, wanted) {
			return ref.ID
		}
	}
	return ""
}

func containsFile(files []string, wanted string) bool {
	for _, file := range files {
		if filepath.ToSlash(file) == wanted {
			return true
		}
	}
	return false
}

func extractControllerFileForID(atlasData, controllerID string) string {
	for _, ref := range atlas.EntityRefs(atlasData) {
		if ref.ID == controllerID && (ref.Kind == "controller" || strings.HasPrefix(ref.ID, "controller:")) {
			if ref.Source.File != "" {
				return ref.Source.File
			}
			for _, file := range ref.Files {
				if file != "" && !strings.HasSuffix(file, "_test.go") {
					return file
				}
			}
		}
	}
	return ""
}

// appendGraphSelectedSources reads only source files and spans named by Atlas
// entities. It deliberately does not discover files, parse packages, or infer
// related implementation locations.
func appendGraphSelectedSources(ws *WorkingSet, repoPath string, refs []atlas.EntityRef, relationships []atlas.RelationshipRef, controllerFile, selectedControllerID string) {
	files := make(map[string]bool)
	allowed := make(map[string]bool)
	if selectedControllerID != "" {
		allowed[selectedControllerID] = true
		for _, relationship := range relationships {
			if relationship.From == selectedControllerID && relationship.Type == "calls" && strings.HasPrefix(relationship.To, "function:") {
				allowed[relationship.To] = true
			}
		}
	}
	if controllerFile != "" {
		files[filepath.ToSlash(controllerFile)] = true
	}
	for _, ref := range refs {
		if ref.Kind != "function" && ref.Kind != "controller" {
			continue
		}
		if selectedControllerID != "" && !allowed[ref.ID] {
			continue
		}
		if ref.Source.File != "" && !strings.HasSuffix(ref.Source.File, "_test.go") {
			files[filepath.ToSlash(ref.Source.File)] = true
		}
		for _, file := range ref.Files {
			if file != "" && !strings.HasSuffix(file, "_test.go") {
				files[filepath.ToSlash(file)] = true
			}
		}
	}

	paths := make([]string, 0, len(files))
	for file := range files {
		paths = append(paths, file)
	}
	sort.Strings(paths)

	added := make(map[string]bool)
	for _, file := range paths {
		fullPath, ok := safeRepositoryFile(repoPath, file)
		if !ok {
			continue
		}
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		source := string(content)
		if file == filepath.ToSlash(controllerFile) {
			if code := readFileCapped(fullPath, 160); code != "" {
				ws.ImplFiles = append(ws.ImplFiles, FileContent{Path: file, Code: code})
			}
		}

		for _, ref := range refs {
			if ref.Kind != "function" || filepath.ToSlash(ref.Source.File) != file || ref.Source.Line <= 0 || added[ref.ID] {
				continue
			}
			block := ""
			if ref.Source.EndLine > 0 {
				block = extractLines(source, ref.Source.Line, ref.Source.EndLine)
			}
			if block == "" {
				// Older graph versions did not carry EndLine. Fall back to the
				// function parser rather than silently sending one source line.
				block = extractFuncBlock(source, ref.Name)
			}
			if block == "" || len(block) >= 3000 {
				continue
			}
			name := ref.Name
			if name == "" {
				name = ref.ID[strings.LastIndex(ref.ID, ".")+1:]
			}
			ws.ImplFiles = append(ws.ImplFiles, FileContent{
				Path: file + " → " + name + "()",
				Code: block,
			})
			added[ref.ID] = true
		}
	}
}

func appendLegacyFunctionSource(ws *WorkingSet, repoPath, controllerFile string, functions []string) {
	fullPath, ok := safeRepositoryFile(repoPath, controllerFile)
	if !ok {
		return
	}
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return
	}
	source := string(content)
	for _, fn := range functions {
		block := extractFuncBlock(source, fn)
		if block != "" && len(block) < 3000 {
			ws.ImplFiles = append(ws.ImplFiles, FileContent{
				Path: filepath.ToSlash(controllerFile) + " → " + fn + "()",
				Code: block,
			})
			return
		}
	}
}

func safeRepositoryFile(repoPath, relativePath string) (string, bool) {
	if repoPath == "" || relativePath == "" {
		return "", false
	}
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	full := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
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

func extractLines(content string, startLine, endLine int) string {
	lines := strings.Split(content, "\n")
	if startLine <= 0 || startLine > len(lines) {
		return ""
	}
	if endLine < startLine || endLine > len(lines) {
		endLine = startLine
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

var testPathPattern = regexp.MustCompile(`(\S+_test\.go)(?::\d+)?`)

func findTestsFromGraph(a atlas.Runner, repoPath string, controllerID string, functionIDs []string, atlasData string) []FileContent {
	seen := make(map[string]bool)
	var testPaths []string
	structured := false
	if _, structured = a.(atlas.JSONRunner); structured {
		structured = true
	}

	if structured {
		appendGraphTestPaths(atlasData, controllerID, functionIDs, seen, &testPaths)
	}

	if len(testPaths) == 0 {
		var out string
		var err error
		if structured {
			out, err = runAtlas(a, "ask", controllerID, "--intent", "debug", "--compact")
		} else {
			out, err = runAtlas(a, "investigate", controllerID, "--compact")
		}
		if err == nil {
			if structured {
				appendGraphTestPaths(out, controllerID, functionIDs, seen, &testPaths)
			} else {
				appendTestPaths(out, structured, seen, &testPaths)
			}
		}
	}

	if len(testPaths) == 0 {
		for _, fn := range functionIDs {
			if len(testPaths) >= 3 {
				break
			}
			out, err := runAtlas(a, "impact", fn, "--compact")
			if err != nil {
				continue
			}
			if structured {
				appendGraphTestPaths(out, "", []string{fn}, seen, &testPaths)
			} else {
				appendTestPaths(out, structured, seen, &testPaths)
			}
		}
	}

	var files []FileContent
	for _, p := range testPaths {
		if len(files) >= 3 {
			break
		}
		fullPath, ok := safeRepositoryFile(repoPath, p)
		if !ok {
			continue
		}
		code := readFileCapped(fullPath, 200)
		if code != "" {
			files = append(files, FileContent{Path: p, Code: code})
		}
	}
	return files
}

// appendGraphTestPaths reuses only test entities connected by an explicit
// graph relationship to the selected controller or one of its evidenced call
// targets. A test merely appearing in a broad search result is not treated as
// related behavior.
func appendGraphTestPaths(data, controllerID string, functionIDs []string, seen map[string]bool, paths *[]string) {
	allowed := map[string]bool{controllerID: true}
	for _, functionID := range functionIDs {
		allowed[functionID] = true
	}
	refs := make(map[string]atlas.EntityRef)
	for _, ref := range atlas.EntityRefs(data) {
		refs[ref.ID] = ref
	}
	for _, relationship := range atlas.RelationshipRefs(data) {
		candidate := ""
		if allowed[relationship.From] {
			candidate = relationship.To
		} else if allowed[relationship.To] {
			candidate = relationship.From
		}
		if !strings.HasPrefix(candidate, "test:") || seen[candidate] {
			continue
		}
		ref, ok := refs[candidate]
		if !ok || !strings.HasSuffix(ref.Source.File, "_test.go") {
			continue
		}
		seen[candidate] = true
		*paths = append(*paths, ref.Source.File)
	}
}

func appendTestPaths(data string, structured bool, seen map[string]bool, paths *[]string) {
	if structured {
		for _, ref := range atlas.EntityRefs(data) {
			if ref.Kind != "test" && !strings.HasPrefix(ref.ID, "test:") {
				continue
			}
			if strings.HasSuffix(ref.Source.File, "_test.go") && !seen[ref.Source.File] {
				seen[ref.Source.File] = true
				*paths = append(*paths, ref.Source.File)
			}
		}
		return
	}
	for _, match := range testPathPattern.FindAllStringSubmatch(data, -1) {
		p := match[1]
		if !seen[p] {
			seen[p] = true
			*paths = append(*paths, p)
		}
	}
}

func runAtlas(a atlas.Runner, args ...string) (string, error) {
	if jr, ok := a.(atlas.JSONRunner); ok {
		return jr.RunJSON(args...)
	}
	return a.Run(args...)
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

func limitWorkingSet(ws *WorkingSet, maxChars int) {
	if maxChars <= 0 {
		return
	}
	remaining := maxChars
	ws.Types = limitText(ws.Types, &remaining)
	for i := range ws.ImplFiles {
		ws.ImplFiles[i].Code = limitText(ws.ImplFiles[i].Code, &remaining)
	}
	for i := range ws.TestFiles {
		ws.TestFiles[i].Code = limitText(ws.TestFiles[i].Code, &remaining)
	}
}

func limitText(text string, remaining *int) string {
	if *remaining <= 0 {
		return ""
	}
	if len(text) <= *remaining {
		*remaining -= len(text)
		return text
	}
	const marker = "\n// ... working set truncated at the global context budget"
	if *remaining <= len(marker) {
		result := text[:*remaining]
		*remaining = 0
		return result
	}
	limit := *remaining - len(marker)
	if newline := strings.LastIndex(text[:limit], "\n"); newline > 0 {
		limit = newline
	}
	*remaining = 0
	return text[:limit] + marker
}
