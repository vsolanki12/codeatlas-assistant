// Package benchmark measures the context reduction provided by CodeAtlas
// responses. It performs no model generation and never walks a repository.
package benchmark

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/metrics"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
)

const (
	maxRawBytesPerFile = 64 * 1024
	maxRawBytes        = 128 * 1024
)

// Result contains size measurements for one exact entity query. Estimated
// tokens use a deliberately conservative character heuristic; Ollama's
// generation response remains the source of actual prompt/output counts.
type Result struct {
	Entity     string `json:"entity"`
	Intent     string `json:"intent"`
	AtlasCalls int    `json:"atlasCalls"`

	RawRepository *metrics.Text `json:"rawRepository,omitempty"`
	FullAtlas     metrics.Text  `json:"fullAtlas"`
	CompactAtlas  metrics.Text  `json:"compactAtlas"`
	RawPrompt     *metrics.Text `json:"rawPrompt,omitempty"`
	FullPrompt    metrics.Text  `json:"fullPrompt"`
	CompactPrompt metrics.Text  `json:"compactPrompt"`

	CompactVsFullReductionPercent float64  `json:"compactVsFullReductionPercent"`
	CompactVsRawReductionPercent  float64  `json:"compactVsRawReductionPercent,omitempty"`
	Files                         []string `json:"files,omitempty"`
	RawTruncated                  bool     `json:"rawTruncated,omitempty"`
}

// Run compares one full and one compact debug response for entity. The raw
// source baseline is limited to files explicitly named by the full graph
// response; it is not a repository scan and is not presented as Atlas proof.
func Run(a atlas.Runner, entity, repoPath string) (Result, error) {
	if strings.TrimSpace(entity) == "" {
		return Result{}, fmt.Errorf("benchmark entity is required")
	}

	full, err := runJSON(a, "ask", entity, "--intent", "debug")
	if err != nil {
		return Result{}, fmt.Errorf("full Atlas query: %w", err)
	}
	compact, err := runJSON(a, "ask", entity, "--intent", "debug", "--compact")
	if err != nil {
		return Result{}, fmt.Errorf("compact Atlas query: %w", err)
	}

	result := Result{
		Entity:        entity,
		Intent:        "debug",
		AtlasCalls:    2,
		FullAtlas:     metrics.Measure(full),
		CompactAtlas:  metrics.Measure(compact),
		FullPrompt:    metrics.Measure(prompt.BuildAsk("explain "+entity, full, "investigate")),
		CompactPrompt: metrics.Measure(prompt.BuildAsk("explain "+entity, compact, "investigate")),
	}
	result.CompactVsFullReductionPercent = metrics.ReductionPercent(result.FullPrompt.EstimatedTokens, result.CompactPrompt.EstimatedTokens)

	if repoPath == "" {
		repoPath = graphRepository(full)
	}
	// A relative repository value is ambiguous because the graph does not
	// record the process working directory. Require --repo for a safe raw
	// source comparison instead of reading a coincidentally named directory.
	if !filepath.IsAbs(repoPath) {
		repoPath = ""
	}
	raw, files, truncated := readGraphSelectedSource(repoPath, atlas.EntityRefs(full))
	if raw != "" {
		measurement := metrics.Measure(raw)
		result.RawRepository = &measurement
		rawPrompt := prompt.BuildAsk("explain "+entity, "## Repository source baseline\n"+raw, "investigate")
		rawPromptMeasurement := metrics.Measure(rawPrompt)
		result.RawPrompt = &rawPromptMeasurement
		result.CompactVsRawReductionPercent = metrics.ReductionPercent(rawPromptMeasurement.EstimatedTokens, result.CompactPrompt.EstimatedTokens)
	}
	result.Files = files
	result.RawTruncated = truncated
	return result, nil
}

// Format returns a concise human-facing report suitable for a terminal.
func (r Result) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CodeAtlas payload-size diagnostic (does not measure answer correctness or end-to-end savings)\nentity: %s\nintent: %s\nAtlas calls: %d\n", r.Entity, r.Intent, r.AtlasCalls)
	if r.RawRepository != nil {
		fmt.Fprintf(&b, "raw repository context: %s\n", metrics.FormatText(*r.RawRepository))
	}
	fmt.Fprintf(&b, "full Atlas response: %s\n", metrics.FormatText(r.FullAtlas))
	fmt.Fprintf(&b, "compact Atlas response: %s\n", metrics.FormatText(r.CompactAtlas))
	if r.RawPrompt != nil {
		fmt.Fprintf(&b, "raw-source prompt estimate: %s\n", metrics.FormatText(*r.RawPrompt))
	}
	fmt.Fprintf(&b, "full Atlas prompt estimate: %s\n", metrics.FormatText(r.FullPrompt))
	fmt.Fprintf(&b, "compact Atlas prompt estimate: %s\n", metrics.FormatText(r.CompactPrompt))
	fmt.Fprintf(&b, "compact vs full prompt reduction: %.1f%%\n", r.CompactVsFullReductionPercent)
	if r.RawPrompt != nil {
		fmt.Fprintf(&b, "compact vs raw-source prompt reduction: %.1f%%\n", r.CompactVsRawReductionPercent)
	}
	if len(r.Files) > 0 {
		fmt.Fprintf(&b, "graph-selected source files: %d\n", len(r.Files))
	}
	if r.RawTruncated {
		b.WriteString("raw source: truncated at benchmark limit\n")
	}
	b.WriteString("token estimates use ~4 Unicode characters/token; Ollama reports actual counts during generation.\n")
	return b.String()
}

func runJSON(a atlas.Runner, args ...string) (string, error) {
	if runner, ok := a.(atlas.JSONRunner); ok {
		return runner.RunJSON(args...)
	}
	return a.Run(append(args, "--json")...)
}

func graphRepository(data string) string {
	var envelope struct {
		Graph struct {
			Repository string `json:"repository"`
		} `json:"graph"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err == nil {
		return envelope.Graph.Repository
	}
	return ""
}

func readGraphSelectedSource(repoPath string, refs []atlas.EntityRef) (string, []string, bool) {
	if repoPath == "" {
		return "", nil, false
	}
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return "", nil, false
	}

	paths := make(map[string]bool)
	for _, ref := range refs {
		if ref.Source.File != "" {
			paths[filepath.ToSlash(ref.Source.File)] = true
		}
		for _, file := range ref.Files {
			if file != "" {
				paths[filepath.ToSlash(file)] = true
			}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	var source strings.Builder
	var included []string
	truncated := false
	for _, relative := range ordered {
		fullPath, ok := safeRepositoryFile(root, relative)
		if !ok {
			continue
		}
		file, err := os.Open(fullPath)
		if err != nil {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(file, maxRawBytesPerFile+1))
		_ = file.Close()
		if readErr != nil || len(content) == 0 {
			continue
		}
		if len(content) > maxRawBytesPerFile {
			content = content[:maxRawBytesPerFile]
			truncated = true
		}
		section := fmt.Sprintf("### %s\n%s\n", relative, content)
		if source.Len()+len(section) > maxRawBytes {
			remaining := maxRawBytes - source.Len()
			if remaining > 0 {
				source.WriteString(section[:remaining])
			}
			truncated = true
			break
		}
		source.WriteString(section)
		included = append(included, relative)
	}
	return source.String(), included, truncated
}

func safeRepositoryFile(repoPath, relativePath string) (string, bool) {
	clean := filepath.Clean(filepath.FromSlash(relativePath))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	full := filepath.Join(repoPath, clean)
	rel, err := filepath.Rel(repoPath, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}
