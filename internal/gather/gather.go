package gather

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/intent"
	"github.com/vsolanki12/codeatlas-assistant/internal/style"
)

var entityIDPattern = regexp.MustCompile(`(?:controller|function|crd|package|test|document):[a-zA-Z0-9._]+`)
var crdIDPattern = regexp.MustCompile(`crd:[a-zA-Z0-9._]+`)
var controllerWithPathPattern = regexp.MustCompile(`(controller:[a-zA-Z0-9._]+)\s*\|\s*([^\s|]+)`)

type ControllerInfo struct {
	ID   string
	File string
	Role string // "workload" or "api"
}

type Result struct {
	AtlasData   string
	StyleCode   string
	Terms       []string
	Controllers []ControllerInfo
}

func FromJIRA(a atlas.Runner, jiraText string) Result {
	fmt.Fprintln(os.Stderr, "--- Extracting technical terms ---")
	terms := intent.ExtractTechnicalTerms(jiraText)

	if len(terms) == 0 {
		fmt.Fprintln(os.Stderr, "no technical terms found in JIRA text")
		return Result{}
	}

	if len(terms) > 8 {
		terms = terms[:8]
	}

	fmt.Fprintf(os.Stderr, "terms: %s\n", strings.Join(terms, ", "))

	fmt.Fprintln(os.Stderr, "--- Searching atlas ---")
	var atlasData strings.Builder

	for _, term := range terms {
		result, err := a.Run("search", term)
		if err != nil || strings.Contains(result, "No matching") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Search: %s\n%s\n", term, result))
	}

	if atlasData.Len() == 0 {
		return Result{Terms: terms}
	}

	deepDiveCount := 3
	if len(terms) < deepDiveCount {
		deepDiveCount = len(terms)
	}

	for i := 0; i < deepDiveCount; i++ {
		term := terms[i]
		fmt.Fprintf(os.Stderr, "--- Deep dive: %s ---\n", term)

		explainResult, err := a.Run("explain", term)
		if err == nil && !strings.Contains(explainResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Explain: %s\n%s\n", term, explainResult))
		}

		investigateResult, err := a.Run("investigate", term)
		if err == nil && !strings.Contains(investigateResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Investigate: %s\n%s\n", term, investigateResult))
		}
	}

	discoverControllers(a, &atlasData)
	expandRelatedEntities(a, terms, &atlasData)

	if atlasData.Len() > 40000 {
		fmt.Fprintf(os.Stderr, "atlas data: %d chars (capped to 40000)\n", atlasData.Len())
		truncated := atlasData.String()[:40000]
		atlasData.Reset()
		atlasData.WriteString(truncated)
		atlasData.WriteString("\n... (truncated)\n")
	}

	styleCode := style.LoadReference("", atlasData.String(), a.GraphPath())
	if styleCode != "" {
		fmt.Fprintln(os.Stderr, "--- Style reference loaded ---")
	}

	controllers := extractControllers(atlasData.String(), a)
	rankControllers(controllers, terms)

	return Result{
		AtlasData:   atlasData.String(),
		StyleCode:   styleCode,
		Terms:       terms,
		Controllers: controllers,
	}
}

func extractControllers(data string, a atlas.Runner) []ControllerInfo {
	matches := controllerWithPathPattern.FindAllStringSubmatch(data, -1)
	seen := make(map[string]bool)
	var controllers []ControllerInfo
	for _, m := range matches {
		id, file := m[1], m[2]
		if idx := strings.LastIndex(file, ":"); idx > 0 {
			file = file[:idx]
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		role := classifyController(a, id)
		controllers = append(controllers, ControllerInfo{ID: id, File: file, Role: role})
	}
	return controllers
}

func rankControllers(controllers []ControllerInfo, terms []string) {
	sort.SliceStable(controllers, func(i, j int) bool {
		si := controllerTermScore(controllers[i], terms)
		sj := controllerTermScore(controllers[j], terms)
		if controllers[i].Role != controllers[j].Role {
			return controllers[i].Role == "workload"
		}
		return si > sj
	})
}

func controllerTermScore(c ControllerInfo, terms []string) int {
	target := strings.ToLower(c.ID + " " + c.File)
	score := 0
	for _, t := range terms {
		if strings.Contains(target, strings.ToLower(t)) {
			score++
		}
	}
	return score
}

func classifyController(a atlas.Runner, controllerID string) string {
	name := controllerID
	if idx := strings.LastIndex(name, "."); idx != -1 {
		name = name[idx+1:]
	}
	name = strings.TrimPrefix(name, "controller:")

	result, err := a.Run("investigate", name)
	if err != nil || result == "" {
		return "unknown"
	}
	lower := strings.ToLower(result)
	if strings.Contains(lower, "creates") &&
		(strings.Contains(lower, "deployment") ||
			strings.Contains(lower, "statefulset") ||
			strings.Contains(lower, "daemonset")) {
		return "workload"
	}
	return "api"
}

func discoverControllers(a atlas.Runner, atlasData *strings.Builder) {
	collected := atlasData.String()
	crdIDs := crdIDPattern.FindAllString(collected, -1)
	if len(crdIDs) == 0 {
		return
	}

	seen := make(map[string]bool)
	var unique []string
	for _, id := range crdIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) > 3 {
		unique = unique[:3]
	}

	fmt.Fprintf(os.Stderr, "--- Discovering controllers for %d CRDs ---\n", len(unique))
	for _, crdID := range unique {
		result, err := a.Run("context", crdID, "--depth", "2")
		if err != nil || strings.Contains(result, "Empty subgraph") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Controllers for %s\n%s\n", crdID, result))
		fmt.Fprintf(os.Stderr, "  discovered: %s\n", crdID)
	}
}

func expandRelatedEntities(a atlas.Runner, searchedTerms []string, atlasData *strings.Builder) {
	collected := atlasData.String()
	entityIDs := entityIDPattern.FindAllString(collected, -1)
	if len(entityIDs) == 0 {
		return
	}

	searched := make(map[string]bool)
	for _, t := range searchedTerms {
		searched[strings.ToLower(t)] = true
	}

	codeKinds := map[string]bool{"controller": true, "function": true, "crd": true}

	var novel []string
	seen := make(map[string]bool)
	for _, id := range entityIDs {
		if seen[id] || searched[strings.ToLower(id)] {
			continue
		}
		seen[id] = true
		parts := strings.SplitN(id, ":", 2)
		if len(parts) != 2 || !codeKinds[parts[0]] || searched[strings.ToLower(parts[1])] {
			continue
		}
		novel = append(novel, id)
	}

	if len(novel) == 0 {
		return
	}

	if len(novel) > 5 {
		novel = novel[:5]
	}

	fmt.Fprintf(os.Stderr, "--- Expanding %d related entities ---\n", len(novel))
	for _, id := range novel {
		parts := strings.SplitN(id, ":", 2)
		name := parts[1]
		if idx := strings.LastIndex(name, "."); idx != -1 {
			name = name[idx+1:]
		}

		result, err := a.Run("investigate", name)
		if err != nil || strings.Contains(result, "not found") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Related: %s\n%s\n", id, result))
		fmt.Fprintf(os.Stderr, "  expanded: %s\n", id)
	}
}
