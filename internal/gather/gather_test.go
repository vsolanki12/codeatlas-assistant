package gather

import (
	"fmt"
	"strings"
	"testing"
)

type structuredRunner struct {
	responses map[string]string
	calls     []string
}

func (r *structuredRunner) Run(args ...string) (string, error) {
	return "", fmt.Errorf("unexpected text Atlas call: %s", strings.Join(args, " "))
}

func (r *structuredRunner) RunJSON(args ...string) (string, error) {
	key := strings.Join(args, " ")
	r.calls = append(r.calls, key)
	result, ok := r.responses[key]
	if !ok {
		return "", fmt.Errorf("unexpected Atlas call: %s", key)
	}
	return result, nil
}

func (r *structuredRunner) GraphPath() string { return "" }

func TestSelectWorkloadRequiresUniqueController(t *testing.T) {
	if _, ok := SelectWorkload([]ControllerInfo{{ID: "controller:a", Role: "workload"}, {ID: "controller:b", Role: "workload"}}); ok {
		t.Fatal("multiple workload controllers must remain ambiguous")
	}
	selected, ok := SelectWorkload([]ControllerInfo{{ID: "controller:api", Role: "api"}, {ID: "controller:workload", Role: "workload"}})
	if !ok || selected.ID != "controller:workload" {
		t.Fatalf("unique workload controller was not selected: %+v, %v", selected, ok)
	}
}

func TestSelectImplementationControllerRequiresOneSafeCandidate(t *testing.T) {
	selected, ok := SelectImplementationController([]ControllerInfo{{ID: "controller:api", Role: "unknown"}})
	if !ok || selected.ID != "controller:api" {
		t.Fatalf("unique controller should be selectable for implementation: %+v, %v", selected, ok)
	}
	if _, ok := SelectImplementationController([]ControllerInfo{{ID: "controller:a", Role: "unknown"}, {ID: "controller:b", Role: "api"}}); ok {
		t.Fatal("multiple non-workload controllers must remain ambiguous")
	}
}

func TestExtractControllersReusesRelationshipEvidence(t *testing.T) {
	data := `{"entities":[{"id":"controller:example.com/repo/pkg.Reconciler","name":"Reconciler","kind":"controller","source":{"file":"controllers/reconcile.go","line":3}}],"relationships":[{"id":"controller:example.com/repo/pkg.Reconciler--creates--resource:apps/Deployment","from":"controller:example.com/repo/pkg.Reconciler","to":"resource:apps/Deployment","type":"creates","confidence":"proven","evidence":{"file":"controllers/reconcile.go","line":3}}]}`
	runner := &structuredRunner{}
	controllers := extractControllers(data, runner)
	if len(controllers) != 1 || controllers[0].Role != "workload" {
		t.Fatalf("controller was not classified from existing evidence: %+v", controllers)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("classification re-queried Atlas: %v", runner.calls)
	}
}

func TestFromJIRAStructuredPathAvoidsCompatibilityExpansion(t *testing.T) {
	entityID := "controller:example.com/repo/pkg.Reconciler"
	runner := &structuredRunner{responses: map[string]string{
		"search NodePool --compact":             fmt.Sprintf(`{"entities":[{"id":%q,"name":"Reconciler","kind":"controller","source":{"file":"controllers/reconcile.go","line":3}}]}`, entityID),
		"ask NodePool --intent debug --compact": fmt.Sprintf(`{"entities":[{"id":%q,"name":"Reconciler","kind":"controller","source":{"file":"controllers/reconcile.go","line":3}}],"relationships":[{"id":"%s--creates--resource:apps/Deployment","from":%q,"to":"resource:apps/Deployment","type":"creates","confidence":"proven","evidence":{"file":"controllers/reconcile.go","line":3}}]}`, entityID, entityID, entityID),
	}}

	result := FromJIRA(runner, "NodePool")
	if len(result.Controllers) != 1 || result.Controllers[0].Role != "workload" {
		t.Fatalf("unexpected gathered controller result: %+v", result.Controllers)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("structured gathering made compatibility expansion calls: %v", runner.calls)
	}
	if runner.calls[0] != "search NodePool --compact" || runner.calls[1] != "ask NodePool --intent debug --compact" {
		t.Fatalf("unexpected structured gathering call sequence: %v", runner.calls)
	}
}
