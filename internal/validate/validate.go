package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
)

type Violation struct {
	Kind string // "path", "entity"
	Ref  string // the reference that failed
	Why  string
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

var goPathPattern = regexp.MustCompile(`(?:^|\s|[-|:])([a-zA-Z0-9_/.-]+\.go)\b`)

// Output validates LLM output by checking file path references
// against the repo filesystem and/or atlas graph.
func Output(text string, repoPath string, a atlas.Runner) Result {
	var r Result

	paths := extractGoPaths(text)
	for _, p := range paths {
		r.Checked++

		if repoPath != "" {
			full := filepath.Join(repoPath, p)
			if _, err := os.Stat(full); err == nil {
				continue
			}
		}

		if a != nil {
			out, err := a.Run("where", p)
			if err == nil && !strings.Contains(out, "0 entities") && strings.TrimSpace(out) != "" {
				continue
			}
		}

		why := "not found in repo"
		if a != nil {
			why += " or graph"
		}
		r.Violations = append(r.Violations, Violation{Kind: "path", Ref: p, Why: why})
	}

	return r
}

// ClaudeXML validates structured Claude XML output, checking <files>
// and <functions> sections against repo and graph.
func ClaudeXML(xml string, repoPath string, a atlas.Runner) Result {
	var r Result

	filePaths := extractXMLSection(xml, "files")
	for _, line := range filePaths {
		p := extractPathFromLine(line)
		if p == "" {
			continue
		}
		r.Checked++

		if repoPath != "" {
			full := filepath.Join(repoPath, p)
			if _, err := os.Stat(full); err == nil {
				continue
			}
		}

		if a != nil {
			out, err := a.Run("where", p)
			if err == nil && !strings.Contains(out, "0 entities") && strings.TrimSpace(out) != "" {
				continue
			}
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

		if a != nil {
			out, err := a.Run("search", name)
			if err == nil && !strings.Contains(out, "No matching") && strings.TrimSpace(out) != "" {
				continue
			}
		}

		r.Violations = append(r.Violations, Violation{
			Kind: "entity",
			Ref:  name,
			Why:  "function not found in graph",
		})
	}

	return r
}

func extractGoPaths(text string) []string {
	matches := goPathPattern.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)
	var paths []string
	for _, m := range matches {
		p := m[1]
		if seen[p] || p == "go.mod" || p == "go.sum" {
			continue
		}
		// skip patterns that look like Go package imports, not file paths
		if !strings.Contains(p, "/") && !strings.HasSuffix(p, "_test.go") {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths
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
	line = strings.TrimPrefix(strings.TrimSpace(line), "- ")
	parts := strings.SplitN(line, " ", 2)
	p := parts[0]
	p = strings.TrimSuffix(p, " —")
	p = strings.TrimSuffix(p, " -")
	if strings.HasSuffix(p, ".go") {
		return p
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
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, ".go") {
		return ""
	}
	return name
}
