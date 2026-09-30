package validate

import (
	"strings"
	"testing"
)

type validationRunner struct{}

func (validationRunner) Run(args ...string) (string, error) { return "", nil }
func (validationRunner) GraphPath() string                  { return "atlas.json" }
func (validationRunner) RunJSON(args ...string) (string, error) {
	if len(args) >= 2 && args[0] == "where" && args[1] == "pkg/known.go" {
		return `{"entities":[{"id":"function:example/pkg.Known","name":"Known","kind":"function","source":{"file":"pkg/known.go"}}]}`, nil
	}
	if len(args) >= 2 && args[0] == "where" && args[1] == "config/widget.yaml" {
		return `{"entities":[{"id":"resource:widget._cluster/example@config/widget.yaml","name":"Widget","kind":"resource","source":{"file":"config/widget.yaml"}}]}`, nil
	}
	if len(args) >= 2 && args[0] == "where" && args[1] == "docs/design.md" {
		return `{"entities":[{"id":"document:docs/design.md","name":"design.md","kind":"document","source":{"file":"docs/design.md"}}]}`, nil
	}
	if len(args) >= 2 && args[0] == "ask" && args[1] == "function:example/pkg.Known" {
		return `{"entity":{"id":"function:example/pkg.Known","name":"Known","kind":"function"}}`, nil
	}
	if len(args) >= 2 && args[0] == "ask" && args[1] == "field:example/pkg.Management.AutoRepair" {
		return `{"entity":{"id":"field:example/pkg.Management.AutoRepair","kind":"field"}}`, nil
	}
	if len(args) >= 2 && args[0] == "ask" && args[1] == "Known" {
		return `{"entity":{"id":"function:example/pkg.Known","name":"Known","kind":"function"}}`, nil
	}
	if len(args) >= 2 && args[0] == "search" && args[1] == "Known" {
		return `{"entities":[{"id":"function:example/pkg.Known","name":"Known","kind":"function"}]}`, nil
	}
	return `{"entities":[]}`, nil
}

func TestExtractGoPaths(t *testing.T) {
	text := `Modify these files:
- pkg/controllers/hostedcluster/hostedcluster_controller.go — main reconciler
- control-plane-operator/controllers/hostedcontrolplane/hostedcontrolplane_controller.go
Also check vendor/something.go and standalone.go for reference.`

	paths := extractGoPaths(text)

	want := map[string]bool{
		"pkg/controllers/hostedcluster/hostedcluster_controller.go":                              true,
		"control-plane-operator/controllers/hostedcontrolplane/hostedcontrolplane_controller.go": true,
		"vendor/something.go": true,
		"standalone.go":       true,
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

func TestExtractGoPaths_IncludesBareFilenames(t *testing.T) {
	text := "The file controller.go has the logic."
	paths := extractGoPaths(text)
	if len(paths) != 1 || paths[0] != "controller.go" {
		t.Errorf("expected bare filename to be checked, got %v", paths)
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
		{"- README.md — docs", "README.md"},
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
	if r.Checked != 1 || r.OK() {
		t.Errorf("expected structural violation for non-XML input, got %+v", r)
	}
}

func TestOutputUsesGraphAsAuthority(t *testing.T) {
	text := "Modify pkg/known.go and pkg/unknown.go; use function:example/pkg.Known."
	r := Output(text, "", validationRunner{})
	if r.Checked != 3 {
		t.Fatalf("expected three references checked, got %d", r.Checked)
	}
	if len(r.Violations) != 1 || r.Violations[0].Ref != "pkg/unknown.go" {
		t.Fatalf("expected only unknown path to fail, got %+v", r.Violations)
	}
}

func TestOutputValidatesAllScannerSupportedPaths(t *testing.T) {
	text := "Modify config/widget.yaml and docs/design.md; do not use config/missing.yaml."
	r := Output(text, "", validationRunner{})
	if r.Checked != 3 {
		t.Fatalf("expected three repository paths checked, got %d", r.Checked)
	}
	if len(r.Violations) != 1 || r.Violations[0].Ref != "config/missing.yaml" {
		t.Fatalf("expected only unknown YAML path to fail, got %+v", r.Violations)
	}
}

func TestExtractAtlasIDsAcceptsTemplateIdentity(t *testing.T) {
	ids := extractAtlasIDs("use template:kubernetes.deployment@config/deployment.yaml#1 as the unresolved manifest identity")
	if len(ids) != 1 || ids[0] != "template:kubernetes.deployment@config/deployment.yaml#1" {
		t.Fatalf("template IDs = %v, want exact template identity", ids)
	}
}

func TestOutputChecksMarkdownPathsAndNewFieldIdentities(t *testing.T) {
	text := "Use `pkg/known.go`, [design](docs/design.md), and field:example/pkg.Management.AutoRepair; avoid `pkg/missing.go` and field:example/pkg.Missing.AutoRepair."
	result := Output(text, "", validationRunner{})
	if result.Checked != 5 || len(result.Violations) != 2 {
		t.Fatalf("wrapped paths or field identities bypassed validation: %+v", result)
	}
	if result.Violations[0].Ref != "pkg/missing.go" || result.Violations[1].Ref != "field:example/pkg.Missing.AutoRepair" {
		t.Fatalf("wrong violations: %+v", result.Violations)
	}
}

type maskedEmptyGraphRunner struct{}

func (maskedEmptyGraphRunner) GraphPath() string                 { return "fixture" }
func (maskedEmptyGraphRunner) Run(...string) (string, error)     { return `{"entities":[]}`, nil }
func (maskedEmptyGraphRunner) RunJSON(...string) (string, error) { return `{"entities":[]}`, nil }

func TestOutputNeverAcceptsEmptyJSONAsNonemptyTextEvidence(t *testing.T) {
	result := Output("Inspect `pkg/invented.go`.", "", maskedEmptyGraphRunner{})
	if result.OK() || result.Checked != 1 {
		t.Fatalf("empty structured graph was accepted through text fallback: %+v", result)
	}
}

func TestClaudeXMLRequiresUnambiguousGraphFunction(t *testing.T) {
	xml := `<files>
- pkg/known.go — implementation
</files>
<functions>
- Known — implementation
</functions>
<tests>
- Needed: no existing test identified
</tests>`
	r := ClaudeXML(xml, "", validationRunner{})
	if !r.OK() {
		t.Fatalf("expected graph-backed XML validation to pass, got %+v", r)
	}

	if strings.Contains(r.Report(), "violation") {
		t.Fatalf("unexpected validation report: %s", r.Report())
	}
}

func TestClaudeXMLDoesNotTreatImplementationAsTest(t *testing.T) {
	xml := `<files>
- pkg/known.go — implementation
</files>
<functions>
- Known — implementation
</functions>
<tests>
- pkg/known.go — related behavior
</tests>`
	r := ClaudeXML(xml, "", validationRunner{})
	if r.OK() {
		t.Fatalf("expected implementation file to fail test-specific validation")
	}
	if len(r.Violations) != 1 || r.Violations[0].Ref != "pkg/known.go" {
		t.Fatalf("expected test-file violation, got %+v", r.Violations)
	}
}

func TestClaudeXMLRejectsNonFunctionEntityID(t *testing.T) {
	xml := `<files>
- pkg/known.go — implementation
</files>
<functions>
- controller:example/pkg.Controller — not a function
</functions>
<tests>
- Needed: no existing test identified
</tests>`
	r := ClaudeXML(xml, "", validationRunner{})
	if r.OK() {
		t.Fatalf("expected non-function entity ID to fail function validation")
	}
	if len(r.Violations) != 1 || r.Violations[0].Kind != "entity" {
		t.Fatalf("expected function entity violation, got %+v", r.Violations)
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
