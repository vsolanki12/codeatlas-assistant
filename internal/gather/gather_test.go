package gather

import "testing"

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
