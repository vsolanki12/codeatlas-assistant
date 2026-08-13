package prompt

import (
	"strings"
	"testing"
)

func TestSolveWorkingSet_ContractStrings(t *testing.T) {
	output := BuildWorkingSetSolve(
		"test jira",
		"conventions",
		"controller:TestController",
		[]string{"Reconcile"},
		[]FileContent{{Path: "pkg/a.go", Code: "package a"}},
		[]FileContent{{Path: "pkg/a_test.go", Code: "package a"}},
		"type Foo struct{}",
	)

	required := []string{
		"NEED_MORE_CONTEXT",
		"MUST NOT invent",
		"MUST NOT choose another file",
		"ONLY the files and functions shown above",
	}
	for _, s := range required {
		if !strings.Contains(output, s) {
			t.Errorf("working-set solve prompt missing required constraint: %q", s)
		}
	}
}

func TestSolveLegacy_ContractStrings(t *testing.T) {
	output := BuildSolve("test jira", "atlas data", "", "", "", "")

	required := []string{
		"ONLY reference files",
		"NEVER invent or guess",
		"not found in atlas data",
	}
	for _, s := range required {
		if !strings.Contains(output, s) {
			t.Errorf("legacy solve prompt missing required constraint: %q", s)
		}
	}
}

func TestClaude_ContractStrings(t *testing.T) {
	controllers := []ControllerEntry{
		{ID: "controller:Test", File: "pkg/test.go", Role: "workload"},
	}
	output := BuildClaude("test jira", "atlas data", "", "", "pkg/test.go", "", nil, controllers)

	required := []string{
		"ONLY these paths",
		"Do NOT invent",
		"inferred",
		"verify",
	}
	for _, s := range required {
		if !strings.Contains(output, s) {
			t.Errorf("claude prompt missing required constraint: %q", s)
		}
	}

	forbidden := []string{
		"LOCKED",
		"deterministic",
	}
	for _, s := range forbidden {
		if strings.Contains(output, s) {
			t.Errorf("claude prompt contains forbidden term: %q", s)
		}
	}
}

func TestAsk_ContractStrings(t *testing.T) {
	output := BuildAsk("what does Reconcile do?", "atlas output", "explain")

	required := []string{
		"ONLY the CodeAtlas data",
	}
	for _, s := range required {
		if !strings.Contains(output, s) {
			t.Errorf("ask prompt missing required constraint: %q", s)
		}
	}
}

func TestGenerate_ContractStrings(t *testing.T) {
	output := BuildGenerate("generate a controller", "atlas data", "", "")

	required := []string{
		"EXACT patterns",
		"Only output Go code",
	}
	for _, s := range required {
		if !strings.Contains(output, s) {
			t.Errorf("generate prompt missing required constraint: %q", s)
		}
	}
}
