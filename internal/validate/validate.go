package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
)

type Violation struct {
	Kind string `json:"kind"` // "path", "entity"
	Ref  string `json:"ref"`  // the reference that failed
	Why  string `json:"why"`
}

type Result struct {
	Violations []Violation
	Checked    int
}

func (r Result) OK() bool {
	return len(r.Violations) == 0
}

func (r Result) Report() string {
	if r.OK() {
		return fmt.Sprintf("validation passed: %d references checked", r.Checked)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "validation: %d violations out of %d references\n", len(r.Violations), r.Checked)
	for _, v := range r.Violations {
		fmt.Fprintf(&b, "  [%s] %s — %s\n", v.Kind, v.Ref, v.Why)
	}
	return b.String()
}

// repositoryPathPattern covers every file type currently represented by the
// CodeAtlas scanner. Restricting validation to Go files would allow a model to
// invent a YAML manifest or Markdown design document while still passing the
// output gate.
var repositoryPathPattern = regexp.MustCompile(`(?:^|\s|[-|:])([a-zA-Z0-9_./-]+\.(?:go|ya?ml|md))\b`)
var atlasIDPattern = regexp.MustCompile(`(?:controller|function|crd|package|test|document|resource|template|operator):[a-zA-Z0-9._/@+\-#]+`)

// Output validates LLM output by checking file path references against the
// CodeAtlas graph. A filesystem check is only a fallback when no graph runner
// is available; an existing file that was not scanned is not graph evidence.
func Output(text string, repoPath string, a atlas.Runner) Result {
	var r Result

	paths := extractRepositoryPaths(text)
	for _, p := range paths {
		r.Checked++

		if referenceExists(repoPath, p, a) {
			continue
		}

		why := "not found in repo"
		if a != nil {
			why = "not present in CodeAtlas graph"
		}
		r.Violations = append(r.Violations, Violation{Kind: "path", Ref: p, Why: why})
	}

	for _, id := range extractAtlasIDs(text) {
		r.Checked++
		if a != nil && atlasEntityExists(a, id) {
			continue
		}
		r.Violations = append(r.Violations, Violation{
			Kind: "entity",
			Ref:  id,
			Why:  "entity ID is not present in CodeAtlas graph",
		})
	}

	return r
}

func referenceExists(repoPath, path string, a atlas.Runner) bool {
	if a != nil {
		return atlasPathExists(a, path)
	}
	if repoPath == "" {
		return false
	}
	full, ok := safeRepoPath(repoPath, path)
	if !ok {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && !info.IsDir()
}

func safeRepoPath(repoPath, path string) (string, bool) {
	repoRoot, err := filepath.Abs(repoPath)
	if err != nil {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	full := filepath.Join(repoRoot, clean)
	rel, err := filepath.Rel(repoRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

type atlasEntityRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Source struct {
		File string `json:"file"`
	} `json:"source"`
	Files []string `json:"files"`
}

func decodeAtlasEntities(out string) ([]atlasEntityRef, bool) {
	var entities []atlasEntityRef
	if err := json.Unmarshal([]byte(out), &entities); err == nil {
		return entities, true
	}

	var envelope struct {
		Entities []atlasEntityRef `json:"entities"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err == nil {
		return envelope.Entities, true
	}

	return nil, false
}

func atlasPathExists(a atlas.Runner, path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if jr, ok := a.(atlas.JSONRunner); ok {
		if atlasPageContainsPath(jr, path, false) {
			return true
		}
	}
	out, err := a.Run("where", path)
	return err == nil && !strings.Contains(out, "0 entities") && !strings.Contains(out, "No matching") && strings.TrimSpace(out) != ""
}

func atlasTestPathExists(a atlas.Runner, path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	jr, ok := a.(atlas.JSONRunner)
	if !ok {
		// Text output does not expose a stable kind/source contract.
		return false
	}
	return atlasPageContainsPath(jr, path, true)
}

// atlasPageContainsPath walks the bounded where result until the graph says
// there are no more matches. A single fixed-page lookup could incorrectly
// reject a valid model reference in a high-entity file.
func atlasPageContainsPath(jr atlas.JSONRunner, path string, testsOnly bool) bool {
	const pageLimit = 100
	offset := 0
	for {
		out, err := jr.RunJSON("where", path, "--compact", "--offset", strconv.Itoa(offset), "--limit", strconv.Itoa(pageLimit))
		if err != nil {
			return false
		}
		page, ok := decodeAtlasEntityPage(out)
		if !ok {
			return false
		}
		for _, entity := range page.Entities {
			if testsOnly && entity.Kind != "test" {
				continue
			}
			if entity.Source.File == path || contains(entity.Files, path) {
				return true
			}
		}
		if !page.Truncated || page.NextOffset <= offset {
			return false
		}
		offset = page.NextOffset
	}
}

type atlasEntityPage struct {
	Entities   []atlasEntityRef `json:"entities"`
	NextOffset int              `json:"nextOffset"`
	Truncated  bool             `json:"truncated"`
}

func decodeAtlasEntityPage(out string) (atlasEntityPage, bool) {
	var page atlasEntityPage
	if err := json.Unmarshal([]byte(out), &page); err == nil && page.Entities != nil {
		return page, true
	}
	entities, ok := decodeAtlasEntities(out)
	if !ok {
		return atlasEntityPage{}, false
	}
	return atlasEntityPage{Entities: entities}, true
}

func atlasEntityExists(a atlas.Runner, id string) bool {
	if a == nil {
		return false
	}
	if jr, ok := a.(atlas.JSONRunner); ok {
		out, err := jr.RunJSON("ask", id, "--compact")
		if err != nil {
			return false
		}
		for _, entity := range atlas.EntityRefs(out) {
			if entity.ID == id {
				return true
			}
		}
		return false
	}
	// Text output does not expose a stable entity identity contract. Fail
	// closed instead of accepting a fuzzy search result for an explicit ID.
	return false
}

func atlasFunctionExists(a atlas.Runner, name string) bool {
	if jr, ok := a.(atlas.JSONRunner); ok {
		// Ask resolves an exact name or returns an explicit ambiguity result;
		// shares the same function name.
		if out, err := jr.RunJSON("ask", name, "--compact"); err == nil {
			var result struct {
				Ambiguous bool           `json:"ambiguous"`
				Entity    atlasEntityRef `json:"entity"`
			}
			if json.Unmarshal([]byte(out), &result) == nil && !result.Ambiguous {
				return result.Entity.ID != "" && result.Entity.Kind == "function" && result.Entity.Name == name
			}
		}
	}
	return false
}

func atlasFunctionIDExists(a atlas.Runner, id string) bool {
	if a == nil {
		return false
	}
	jr, ok := a.(atlas.JSONRunner)
	if !ok {
		return false
	}
	out, err := jr.RunJSON("ask", id, "--compact")
	if err != nil {
		return false
	}
	for _, entity := range atlas.EntityRefs(out) {
		if entity.ID == id && entity.Kind == "function" {
			return true
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// ClaudeXML validates structured Claude XML output, checking <files>
// and <functions> sections against repo and graph.
func ClaudeXML(xml string, repoPath string, a atlas.Runner) Result {
	var r Result
	if !strings.Contains(xml, "<files>") || !strings.Contains(xml, "</files>") ||
		!strings.Contains(xml, "<functions>") || !strings.Contains(xml, "</functions>") ||
		!strings.Contains(xml, "<tests>") || !strings.Contains(xml, "</tests>") {
		r.Checked++
		r.Violations = append(r.Violations, Violation{
			Kind: "structure",
			Ref:  "<files>/<functions>/<tests>",
			Why:  "structured Claude output is missing required sections",
		})
		return r
	}

	filePaths := extractXMLSection(xml, "files")
	for _, line := range filePaths {
		p := extractPathFromLine(line)
		if p == "" {
			continue
		}
		r.Checked++

		if referenceExists(repoPath, p, a) {
			continue
		}

		r.Violations = append(r.Violations, Violation{
			Kind: "path",
			Ref:  p,
			Why:  "file not found in repo or graph",
		})
	}

	funcNames := extractXMLSection(xml, "functions")
	for _, line := range funcNames {
		name := extractFuncFromLine(line)
		if name == "" {
			continue
		}
		r.Checked++

		if strings.Contains(name, ":") {
			if a != nil && atlasFunctionIDExists(a, name) {
				continue
			}
		} else if a != nil && atlasFunctionExists(a, name) {
			continue
		}

		r.Violations = append(r.Violations, Violation{
			Kind: "entity",
			Ref:  name,
			Why:  "function not found in graph",
		})
	}

	for _, line := range extractXMLSection(xml, "tests") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if strings.HasPrefix(trimmed, "Needed:") {
			continue
		}
		p := extractPathFromLine(line)
		if p == "" {
			continue
		}
		r.Checked++
		if testReferenceExists(repoPath, p, a) {
			continue
		}
		r.Violations = append(r.Violations, Violation{
			Kind: "path",
			Ref:  p,
			Why:  "test file not found in repo or graph",
		})
	}

	return r
}

func testReferenceExists(repoPath, path string, a atlas.Runner) bool {
	if a != nil {
		return atlasTestPathExists(a, path)
	}
	if repoPath == "" || !strings.HasSuffix(path, "_test.go") {
		return false
	}
	full, ok := safeRepoPath(repoPath, path)
	if !ok {
		return false
	}
	info, err := os.Stat(full)
	return err == nil && !info.IsDir()
}

func extractRepositoryPaths(text string) []string {
	matches := repositoryPathPattern.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)
	var paths []string
	for _, m := range matches {
		p := m[1]
		if seen[p] || p == "go.mod" || p == "go.sum" {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths
}

// extractGoPaths is retained as a source-compatible helper for existing
// callers/tests. Its validation scope now includes all scanner-supported file
// types; the old name reflects the original Go-only implementation.
func extractGoPaths(text string) []string {
	return extractRepositoryPaths(text)
}

func extractAtlasIDs(text string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, id := range atlasIDPattern.FindAllString(text, -1) {
		id = strings.TrimRight(id, ".,;:)]}")
		if id == "" {
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func extractXMLSection(xml, tag string) []string {
	start := strings.Index(xml, "<"+tag+">")
	end := strings.Index(xml, "</"+tag+">")
	if start == -1 || end == -1 || end <= start {
		return nil
	}

	content := xml[start+len(tag)+2 : end]
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && strings.HasPrefix(trimmed, "-") {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func extractPathFromLine(line string) string {
	if match := repositoryPathPattern.FindStringSubmatch(line); len(match) > 1 {
		return match[1]
	}
	return ""
}

func extractFuncFromLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "-")
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	parts := strings.SplitN(line, " ", 2)
	name := parts[0]
	name = strings.TrimSuffix(name, "()")
	if strings.Contains(name, ":") {
		return name
	}
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, ".go") {
		return ""
	}
	return name
}
