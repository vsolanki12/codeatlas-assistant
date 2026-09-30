package workingset

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
)

const DefaultSourceBudget = atlas.DefaultSourceBudget

type selectedSpan struct {
	file              string
	start, end, order int
	role              string
	ids               []string
	reasons           []string
	lineage           []atlas.EvidenceSource
	focus             int
	focused           bool
}

// FromEvidence reads the exact spans selected by Atlas. Callers verify graph
// freshness first. No AST parsing, file discovery, or relationship inference
// occurs here. Overlapping spans are read once and budgets are shared across
// source roles so that implementation cannot consume all test context.
func FromEvidence(repoPath string, packet *atlas.EvidencePacket, budget int) (*WorkingSet, error) {
	if packet == nil {
		return nil, fmt.Errorf("Atlas evidence packet is required")
	}
	if err := packet.RequireEvidence(); err != nil {
		return nil, err
	}
	if budget <= 0 {
		budget = DefaultSourceBudget
	}
	if repoPath == "" {
		repoPath = packet.Repository()
	}
	if repoPath == "" {
		return nil, fmt.Errorf("a repository checkout is required to materialize evidence")
	}
	root, err := filepath.EvalSymlinks(repoPath)
	if err != nil {
		return nil, fmt.Errorf("evidence checkout: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if graphRepo := packet.Repository(); graphRepo != "" {
		graphRoot, err := filepath.EvalSymlinks(graphRepo)
		if err != nil {
			return nil, fmt.Errorf("evidence graph repository: %w", err)
		}
		graphRoot, err = filepath.Abs(graphRoot)
		if err != nil || graphRoot != root {
			return nil, fmt.Errorf("evidence graph does not describe the selected repository")
		}
	}
	ws := &WorkingSet{GraphFingerprint: packet.GraphFingerprint}
	entities := make(map[string]atlas.EvidenceEntity)
	for _, entity := range packet.Entities {
		entities[entity.ID] = entity
	}
	linesByFile := make(map[string][]string)
	unavailable := make(map[string]bool)
	var spans []selectedSpan
	for order, source := range packet.Sources {
		if _, exists := entities[source.EntityID]; !exists {
			return nil, fmt.Errorf("evidence source has unknown entity %q", source.EntityID)
		}
		end := source.Source.EndLine
		if end == 0 {
			end = source.Source.Line
		}
		source.Source.EndLine = end
		if source.Source.Line < 1 || end < source.Source.Line {
			return nil, fmt.Errorf("invalid evidence span for %q", source.EntityID)
		}
		if source.FocusLine != 0 && (source.FocusLine < source.Source.Line || source.FocusLine > end) {
			return nil, fmt.Errorf("invalid evidence focus for %q", source.EntityID)
		}
		role := materializedRole(source)
		span := selectedSpan{file: source.Source.File, start: source.Source.Line, end: end, order: order, role: role,
			ids: []string{source.EntityID}, reasons: []string{source.Reason}, lineage: []atlas.EvidenceSource{source}, focus: source.Source.Line}
		span.focused = source.Role == "usage" || source.Role == "relationship"
		if span.focused {
			span.focus = materializedFocus(source, packet)
		}
		if unavailable[span.file] {
			continue
		}
		if _, exists := linesByFile[span.file]; !exists {
			file, ok := safeRepositoryFile(root, span.file)
			if !ok {
				return nil, fmt.Errorf("unsafe evidence path %q", span.file)
			}
			resolved, err := filepath.EvalSymlinks(file)
			if err != nil {
				ws.Omissions = append(ws.Omissions, fmt.Sprintf("unavailable source: %s", span.file))
				unavailable[span.file] = true
				continue
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("evidence source escapes repository: %s", span.file)
			}
			content, err := os.ReadFile(resolved)
			if err != nil {
				ws.Omissions = append(ws.Omissions, fmt.Sprintf("unreadable source: %s", span.file))
				unavailable[span.file] = true
				continue
			}
			linesByFile[span.file] = strings.Split(string(content), "\n")
		}
		lines := linesByFile[span.file]
		if span.end > len(lines) {
			ws.Omissions = append(ws.Omissions, fmt.Sprintf("unavailable span: %s:%d-%d", span.file, span.start, span.end))
			continue
		}
		if strings.TrimSpace(strings.Join(lines[span.start-1:span.end], "\n")) == "" {
			continue
		}
		spans = append(spans, span)
	}
	roleSet := make(map[string]bool)
	for _, span := range spans {
		roleSet[span.role] = true
	}
	if len(roleSet) == 0 {
		return nil, fmt.Errorf("no usable source remained after materializing Atlas evidence")
	}
	// Retain at least the declaration or graph-evidenced focus line of each
	// span before filling larger bodies. Merging before this step would make
	// a later branch compete with the beginning of an enclosing function.
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].focused != spans[j].focused {
			return spans[i].focused
		}
		return spans[i].order < spans[j].order
	})
	roleBudget := (budget - len("## Verified graph-selected source\n")) / len(roleSet)
	chosen := make(map[int]selectedSpan)
	for _, span := range spans {
		minimum := retainedSpan(span, span.focus, span.focus)
		trial := copyRetainedSpans(chosen)
		trial[span.order] = minimum
		if materializedRoleBytes(trial, span.role, linesByFile, packet) <= roleBudget {
			chosen = trial
		}
	}
	for _, span := range spans {
		minimum, exists := chosen[span.order]
		if !exists {
			continue
		}
		trial := copyRetainedSpans(chosen)
		trial[span.order] = span
		if materializedRoleBytes(trial, span.role, linesByFile, packet) <= roleBudget {
			chosen = trial
			continue
		}
		// Retain whole lines and expand around focused usage instead of cutting
		// a merged prefix. Generic declarations grow from their selected start.
		best := minimum
		if span.focused {
			for radius := 1; ; radius++ {
				start, end := span.focus-radius, span.focus+radius
				if start < span.start {
					start = span.start
				}
				if end > span.end {
					end = span.end
				}
				candidate := retainedSpan(span, start, end)
				trial = copyRetainedSpans(chosen)
				trial[span.order] = candidate
				if materializedRoleBytes(trial, span.role, linesByFile, packet) > roleBudget {
					break
				}
				best = candidate
				if start == span.start && end == span.end {
					break
				}
			}
		} else {
			low, high := span.start, span.end
			for low <= high {
				end := low + (high-low)/2
				candidate := retainedSpan(span, span.start, end)
				trial = copyRetainedSpans(chosen)
				trial[span.order] = candidate
				if materializedRoleBytes(trial, span.role, linesByFile, packet) <= roleBudget {
					best = candidate
					low = end + 1
				} else {
					high = end - 1
				}
			}
		}
		chosen[span.order] = best
	}
	for _, span := range spans {
		retained, exists := chosen[span.order]
		if !exists {
			ws.Omissions = append(ws.Omissions, fmt.Sprintf("source budget omitted %s:%d-%d (%s)", span.file, span.start, span.end, strings.Join(span.ids, ", ")))
			continue
		}
		if retained.start != span.start || retained.end != span.end {
			ws.Omissions = append(ws.Omissions, fmt.Sprintf("source budget retained %s:%d-%d of %d-%d (%s)", span.file, retained.start, retained.end, span.start, span.end, strings.Join(span.ids, ", ")))
		}
	}
	seenFunctions := make(map[string]bool)
	for _, merged := range mergeRetainedSpans(chosen) {
		file := renderRetainedSpan(merged, linesByFile, packet)
		switch merged.role {
		case "test":
			ws.TestFiles = append(ws.TestFiles, file)
		case "definition":
			ws.DefinitionFiles = append(ws.DefinitionFiles, file)
			ws.Types += fmt.Sprintf("### %s\n%s\n```\n%s\n```\n", file.Path, file.Evidence, file.Code)
			ws.TypeFiles = appendUnique(ws.TypeFiles, file.Path)
		default:
			ws.ImplFiles = append(ws.ImplFiles, file)
		}
		for _, source := range merged.lineage {
			// Each entity keeps its own intersection. A helper nested in a large
			// span never inherits another entity's truncated prefix coordinates.
			if source.Source.Line < merged.start {
				source.Source.Line = merged.start
			}
			if source.Source.EndLine > merged.end {
				source.Source.EndLine = merged.end
			}
			if source.Source.Line > source.Source.EndLine {
				continue
			}
			source.Role = merged.role
			ws.Sources = appendMaterializedSource(ws.Sources, source)
			if entities[source.EntityID].Kind == "function" && !seenFunctions[source.EntityID] {
				ws.Functions = append(ws.Functions, source.EntityID)
				seenFunctions[source.EntityID] = true
			}
		}
	}
	if len(ws.ImplFiles) == 0 && len(ws.TestFiles) == 0 && ws.Types == "" {
		return nil, fmt.Errorf("no usable source remained after materializing Atlas evidence")
	}
	return ws, nil
}

func materializedRole(source atlas.EvidenceSource) string {
	if strings.HasSuffix(source.Source.File, "_test.go") {
		return "test"
	}
	if source.Role == "test" || source.Role == "definition" || source.Role == "documentation" {
		return source.Role
	}
	return "implementation"
}

func materializedFocus(source atlas.EvidenceSource, packet *atlas.EvidencePacket) int {
	if source.FocusLine > 0 {
		return source.FocusLine
	}
	end := source.Source.EndLine
	if end == 0 {
		end = source.Source.Line
	}
	for _, edge := range packet.Relationships {
		if edge.From != source.EntityID && edge.To != source.EntityID {
			continue
		}
		if edge.Evidence.File == source.Source.File && edge.Evidence.Line >= source.Source.Line && edge.Evidence.Line <= end {
			return edge.Evidence.Line
		}
	}
	return source.Source.Line + (end-source.Source.Line)/2
}

func retainedSpan(original selectedSpan, start, end int) selectedSpan {
	original.start, original.end = start, end
	original.lineage = append([]atlas.EvidenceSource(nil), original.lineage...)
	for i := range original.lineage {
		original.lineage[i].Source.Line = start
		original.lineage[i].Source.EndLine = end
	}
	return original
}

func copyRetainedSpans(spans map[int]selectedSpan) map[int]selectedSpan {
	copy := make(map[int]selectedSpan, len(spans))
	for order, span := range spans {
		copy[order] = span
	}
	return copy
}

func mergeRetainedSpans(chosen map[int]selectedSpan) []selectedSpan {
	var spans []selectedSpan
	for _, span := range chosen {
		spans = append(spans, span)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].file != spans[j].file {
			return spans[i].file < spans[j].file
		}
		if spans[i].role != spans[j].role {
			return spans[i].role < spans[j].role
		}
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].order < spans[j].order
	})
	var merged []selectedSpan
	for _, span := range spans {
		if len(merged) == 0 || merged[len(merged)-1].file != span.file || merged[len(merged)-1].role != span.role || merged[len(merged)-1].end < span.start-1 {
			span.ids = append([]string(nil), span.ids...)
			span.reasons = append([]string(nil), span.reasons...)
			span.lineage = append([]atlas.EvidenceSource(nil), span.lineage...)
			merged = append(merged, span)
			continue
		}
		last := &merged[len(merged)-1]
		if span.end > last.end {
			last.end = span.end
		}
		if span.order < last.order {
			last.order = span.order
		}
		last.ids = appendUnique(last.ids, span.ids...)
		last.reasons = appendUnique(last.reasons, span.reasons...)
		last.lineage = append(last.lineage, span.lineage...)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].order < merged[j].order })
	return merged
}

func renderRetainedSpan(span selectedSpan, linesByFile map[string][]string, packet *atlas.EvidencePacket) FileContent {
	code := strings.Join(linesByFile[span.file][span.start-1:span.end], "\n")
	labels := make([]string, len(span.ids))
	for i, id := range span.ids {
		labels[i] = packet.EntityLabel(id)
	}
	// The prompt's entity legend and relationship section preserve exact IDs
	// and proof once. Excerpts carry actual retained coordinates and routing.
	evidence := fmt.Sprintf("Atlas source %s:%d-%d; role: %s; entities: %s; selection: %s", span.file, span.start, span.end, span.role, strings.Join(labels, ", "), strings.Join(span.reasons, "; "))
	return FileContent{Path: span.file, Code: code, Evidence: evidence}
}

func materializedRoleBytes(chosen map[int]selectedSpan, role string, linesByFile map[string][]string, packet *atlas.EvidencePacket) int {
	total := 0
	for _, span := range mergeRetainedSpans(chosen) {
		if span.role != role {
			continue
		}
		file := renderRetainedSpan(span, linesByFile, packet)
		total += len(fmt.Sprintf("### %s\n%s\n```\n%s\n```\n", file.Path, file.Evidence, file.Code))
	}
	return total
}

func appendMaterializedSource(sources []atlas.EvidenceSource, source atlas.EvidenceSource) []atlas.EvidenceSource {
	for i, existing := range sources {
		if existing.EntityID != source.EntityID || existing.Role != source.Role || existing.Source.Parser != source.Source.Parser || existing.Source.File != source.Source.File || existing.Source.EndLine < source.Source.Line-1 || source.Source.EndLine < existing.Source.Line-1 {
			continue
		}
		if source.Source.Line < sources[i].Source.Line {
			sources[i].Source.Line = source.Source.Line
		}
		if source.Source.EndLine > sources[i].Source.EndLine {
			sources[i].Source.EndLine = source.Source.EndLine
		}
		if existing.Reason != source.Reason {
			sources[i].Reason = existing.Reason + "; " + source.Reason
		}
		return sources
	}
	return append(sources, source)
}

func appendUnique(values []string, additions ...string) []string {
	for _, value := range additions {
		if value != "" && !containsString(values, value) {
			values = append(values, value)
		}
	}
	return values
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// PromptContext retains omissions and test evidence alongside materialized
// source, so every prompt consumer receives the same qualified working set.
func (ws *WorkingSet) PromptContext() string {
	var out strings.Builder
	out.WriteString("## Verified graph-selected source\n")
	for _, file := range append(append([]FileContent(nil), ws.ImplFiles...), ws.TestFiles...) {
		fmt.Fprintf(&out, "### %s\n%s\n```\n%s\n```\n", file.Path, file.Evidence, file.Code)
	}
	out.WriteString(ws.Types)
	for _, omission := range ws.Omissions {
		fmt.Fprintf(&out, "- source limitation: %s\n", omission)
	}
	return out.String()
}
