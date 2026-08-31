package gather

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/intent"
	"github.com/vsolanki12/codeatlas-assistant/internal/metrics"
	"github.com/vsolanki12/codeatlas-assistant/internal/style"
)

var entityIDPattern = regexp.MustCompile(`(?:controller|function|crd|package|test|document|resource|template):[a-zA-Z0-9._/@+\-#]+`)
var crdIDPattern = regexp.MustCompile(`crd:[a-zA-Z0-9._/@+\-]+`)
var controllerWithPathPattern = regexp.MustCompile(`(controller:[a-zA-Z0-9._/@+\-]+)\s*\|\s*([^\s|]+)`)
var jsonControllerPattern = regexp.MustCompile(`(?s)"id"\s*:\s*"(controller:[^"]+)".{0,700}?"source"\s*:\s*\{.*?"file"\s*:\s*"([^"]+)"`)

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

const maxAtlasData = 24000

// LimitAtlasData applies the same bounded section policy to all
// implementation-oriented workflows. It only removes already retrieved
// Atlas sections; it does not discover or infer repository facts.
func LimitAtlasData(data string, max int) string {
	return limitSections(data, max)
}

// SelectWorkload returns a workload controller only when the graph-backed
// routing result identifies exactly one. Multiple controllers are a real
// ambiguity, not permission for the Assistant to choose one by iteration
// order or display-name similarity.
func SelectWorkload(controllers []ControllerInfo) (ControllerInfo, bool) {
	var selected ControllerInfo
	count := 0
	for _, controller := range controllers {
		if controller.Role != "workload" {
			continue
		}
		selected = controller
		count++
	}
	return selected, count == 1
}

// SelectImplementationController chooses a single graph-backed controller for
// implementation workflows. A uniquely classified workload controller wins;
// when none is classified as workload, one total controller is still safe to
// select. Multiple candidates remain ambiguous and must not be resolved by
// ranking or iteration order.
func SelectImplementationController(controllers []ControllerInfo) (ControllerInfo, bool) {
	selected, uniqueWorkload := SelectWorkload(controllers)
	if uniqueWorkload {
		return selected, true
	}
	if len(controllers) == 1 {
		return controllers[0], true
	}
	return ControllerInfo{}, false
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
		result, err := atlasRun(a, "search", term, "--compact")
		if err != nil || strings.Contains(result, "No matching") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Search: %s\n%s\n", term, result))
	}

	if atlasData.Len() == 0 {
		return Result{Terms: terms}
	}

	_, structured := a.(atlas.JSONRunner)
	deepDiveCount := 3
	if structured {
		// A structured debug Ask already carries the bounded entity and edge
		// context needed by the downstream router. Repeating the same
		// controller/CRD expansion would spend Atlas calls without adding a
		// different evidence source.
		deepDiveCount = 1
	}
	if len(terms) < deepDiveCount {
		deepDiveCount = len(terms)
	}

	for i := 0; i < deepDiveCount; i++ {
		term := terms[i]
		fmt.Fprintf(os.Stderr, "--- Deep dive: %s ---\n", term)

		if structured {
			askResult, err := atlasRun(a, "ask", term, "--intent", "debug", "--compact")
			if err == nil && !atlas.IsAmbiguous(askResult) && !strings.Contains(askResult, "not found") {
				atlasData.WriteString(fmt.Sprintf("### Ask (debug): %s\n%s\n", term, askResult))
			} else if atlas.IsAmbiguous(askResult) {
				fmt.Fprintf(os.Stderr, "  ambiguous Atlas match skipped: %s\n", term)
			}
			continue
		}

		explainResult, err := atlasRun(a, "explain", term)
		if err == nil && !strings.Contains(explainResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Explain: %s\n%s\n", term, explainResult))
		}

		investigateResult, err := atlasRun(a, "investigate", term)
		if err == nil && !strings.Contains(investigateResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Investigate: %s\n%s\n", term, investigateResult))
		}
	}

	if !structured {
		// These compatibility expansions are needed only for old text-output
		// Atlas clients. Current structured Ask results already contain the
		// bounded graph neighborhood and relationship evidence.
		discoverControllers(a, &atlasData)
		expandRelatedEntities(a, terms, &atlasData)
	}

	if atlasData.Len() > maxAtlasData {
		fmt.Fprintf(os.Stderr, "atlas data: %d chars (capped to %d)\n", atlasData.Len(), maxAtlasData)
		data := atlasData.String()
		atlasData.Reset()
		atlasData.WriteString(LimitAtlasData(data, maxAtlasData))
	}
	fmt.Fprintf(os.Stderr, "atlas context: %s\n", metrics.FormatText(metrics.Measure(atlasData.String())))

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
	seen := make(map[string]bool)
	var controllers []ControllerInfo
	add := func(id, file string) {
		if idx := strings.LastIndex(file, ":"); idx > 0 {
			file = file[:idx]
		}
		if seen[id] {
			return
		}
		seen[id] = true
		role, provenByData := classifyControllerFromData(data, id)
		if !provenByData {
			if _, structured := a.(atlas.JSONRunner); structured {
				// A structured result without a qualifying edge does not prove a
				// workload role. Preserve unknown rather than issuing one query
				// per candidate or inferring from names/files.
				role = "unknown"
			} else {
				role = classifyController(a, id)
			}
		}
		controllers = append(controllers, ControllerInfo{ID: id, File: file, Role: role})
	}
	for _, ref := range atlas.EntityRefs(data) {
		if strings.HasPrefix(ref.ID, "controller:") {
			add(ref.ID, ref.Source.File)
		}
	}
	for _, m := range controllerWithPathPattern.FindAllStringSubmatch(data, -1) {
		add(m[1], m[2])
	}
	for _, m := range jsonControllerPattern.FindAllStringSubmatch(data, -1) {
		add(m[1], m[2])
	}
	return controllers
}

func classifyControllerFromData(data, controllerID string) (string, bool) {
	foundRelationship := false
	for _, relationship := range atlas.RelationshipRefs(data) {
		if relationship.From != controllerID {
			continue
		}
		foundRelationship = true
		if relationship.Type == "creates" || relationship.Type == "owns" {
			return "workload", true
		}
	}
	if foundRelationship {
		return "unknown", true
	}
	return "", false
}

func rankControllers(controllers []ControllerInfo, terms []string) {
	sort.SliceStable(controllers, func(i, j int) bool {
		si := controllerTermScore(controllers[i], terms)
		sj := controllerTermScore(controllers[j], terms)
		if controllers[i].Role != controllers[j].Role {
			return controllers[i].Role == "workload"
		}
		if si != sj {
			return si > sj
		}
		return controllers[i].ID < controllers[j].ID
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

// classifyController uses only explicit CodeAtlas relationship edges. The
// assistant does not inspect source files or infer a component topology to
// classify a controller. A creates/owns edge is sufficient to route workload
// context; everything else remains unknown.
func classifyController(a atlas.Runner, controllerID string) string {
	result, err := atlasRun(a, "investigate", controllerID, "--compact")
	if err != nil || result == "" {
		return "unknown"
	}
	for _, relationship := range atlas.RelationshipRefs(result) {
		if relationship.From != controllerID {
			continue
		}
		if relationship.Type == "creates" || relationship.Type == "owns" {
			return "workload"
		}
	}
	return "unknown"
}

func limitSections(data string, max int) string {
	if max <= 0 || len(data) <= max {
		return data
	}
	marker := "\n... (remaining Atlas sections omitted; request exact entity IDs for more context)\n"
	var result strings.Builder
	for _, part := range strings.Split(data, "### ") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		section := "### " + part
		if result.Len()+len(section)+len(marker) > max {
			break
		}
		result.WriteString(section)
	}
	if result.Len() == 0 {
		return marker
	}
	result.WriteString(marker)
	return result.String()
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
		result, err := atlasRun(a, "context", crdID, "--depth", "2", "--compact")
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
		result, err := atlasRun(a, "investigate", id, "--compact")
		if err != nil || strings.Contains(result, "not found") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Related: %s\n%s\n", id, result))
		fmt.Fprintf(os.Stderr, "  expanded: %s\n", id)
	}
}

func atlasRun(a atlas.Runner, args ...string) (string, error) {
	if jr, ok := a.(atlas.JSONRunner); ok {
		return jr.RunJSON(args...)
	}
	return a.Run(args...)
}
