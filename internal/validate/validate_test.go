package validate

import (
	"testing"
)

func TestExtractGoPaths(t *testing.T) {
	text := `Modify these files:
- pkg/controllers/hostedcluster/hostedcluster_controller.go — main reconciler
- control-plane-operator/controllers/hostedcontrolplane/hostedcontrolplane_controller.go
Also check vendor/something.go and standalone.go for reference.`

	paths := extractGoPaths(text)

	want := map[string]bool{
		"pkg/controllers/hostedcluster/hostedcluster_controller.go":                                         true,
		"control-plane-operator/controllers/hostedcontrolplane/hostedcontrolplane_controller.go": true,
		"vendor/something.go": true,
	}

	for _, p := range paths {
		if !want[p] {
			t.Errorf("unexpected path extracted: %q", p)
		}
		delete(want, p)
	}
	for p := range want {
		t.Errorf("expected path not extracted: %q", p)
	}
}

func TestExtractGoPaths_SkipsBareFilenames(t *testing.T) {
	text := "The file controller.go has the logic."
	paths := extractGoPaths(text)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for bare filename, got %v", paths)
	}
}

func TestExtractGoPaths_IncludesTestSuffix(t *testing.T) {
	text := "Run hostedcluster_controller_test.go for tests."
	paths := extractGoPaths(text)
	if len(paths) != 1 || paths[0] != "hostedcluster_controller_test.go" {
		t.Errorf("expected _test.go bare file, got %v", paths)
	}
}

func TestExtractXMLSection(t *testing.T) {
	xml := `<files>
- pkg/controller.go — main file
- pkg/helper.go — utilities
</files>
<functions>
- Reconcile — main loop
- createDeployment — creates workload
</functions>`

	files := extractXMLSection(xml, "files")
	if len(files) != 2 {
		t.Fatalf("expected 2 file lines, got %d", len(files))
	}

	funcs := extractXMLSection(xml, "functions")
	if len(funcs) != 2 {
		t.Fatalf("expected 2 function lines, got %d", len(funcs))
	}
}

func TestExtractPathFromLine(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"- pkg/controller.go — main file", "pkg/controller.go"},
		{"- pkg/helper.go - utilities", "pkg/helper.go"},
		{"- README.md — docs", ""},
		{"- some text without path", ""},
	}

	for _, tc := range tests {
		got := extractPathFromLine(tc.line)
		if got != tc.want {
			t.Errorf("extractPathFromLine(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestExtractFuncFromLine(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"- Reconcile — main loop", "Reconcile"},
		{"- createDeployment() — creates workload", "createDeployment"},
		{"- pkg/file.go — not a function", ""},
		{"- ", ""},
	}

	for _, tc := range tests {
		got := extractFuncFromLine(tc.line)
		if got != tc.want {
			t.Errorf("extractFuncFromLine(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestOutputNoRepo(t *testing.T) {
	text := "Modify pkg/controllers/fake/controller.go to fix the bug."
	r := Output(text, "", nil)
	if r.Checked != 1 {
		t.Errorf("expected 1 checked, got %d", r.Checked)
	}
	if len(r.Violations) != 1 {
		t.Errorf("expected 1 violation (no repo to verify), got %d", len(r.Violations))
	}
}

func TestClaudeXMLEmpty(t *testing.T) {
	r := ClaudeXML("no xml here", "", nil)
	if r.Checked != 0 {
		t.Errorf("expected 0 checked for non-XML input, got %d", r.Checked)
	}
}

func TestResultOK(t *testing.T) {
	r := Result{}
	if !r.OK() {
		t.Error("empty result should be OK")
	}

	r.Violations = append(r.Violations, Violation{Kind: "path", Ref: "fake.go", Why: "not found"})
	if r.OK() {
		t.Error("result with violations should not be OK")
	}
}

func TestResultReport(t *testing.T) {
	r := Result{Checked: 3}
	report := r.Report()
	if report != "validation passed: 3 references checked" {
		t.Errorf("unexpected OK report: %q", report)
	}

	r.Violations = append(r.Violations, Violation{Kind: "path", Ref: "fake.go", Why: "not found"})
	report = r.Report()
	if report == "" {
		t.Error("violation report should not be empty")
	}
}
