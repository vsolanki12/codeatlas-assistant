package gather

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/intent"
)

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
	Packet      *atlas.EvidencePacket
	Error       error
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
	packet, data, err := atlas.QueryEvidence(a, atlas.EvidenceRequest{
		Question: jiraText, Entity: intent.ExactEntityID(jiraText), Intent: "debug",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas error: %v\n", err)
		return Result{Error: err}
	}
	// Atlas owns selection and traversal. Controller metadata is used only to
	// label the prompt; it never initiates another search or restricts spans.
	controllers := extractControllers(data, a)
	return Result{AtlasData: data, Packet: packet, Controllers: controllers}
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
			role = "unknown"
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
