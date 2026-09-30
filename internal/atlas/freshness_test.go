package atlas

import (
	"strings"
	"testing"
)

type mockRunner struct {
	responses map[string]string
}

func (m *mockRunner) Run(args ...string) (string, error) {
	key := strings.Join(args, " ")
	if resp, ok := m.responses[key]; ok {
		return resp, nil
	}
	return "", nil
}

func (m *mockRunner) GraphPath() string { return "test.json" }

type jsonMockRunner struct {
	mockRunner
	json string
}

func (m *jsonMockRunner) RunJSON(args ...string) (string, error) {
	return m.json, nil
}

func TestParseGraphMetadata_JSON(t *testing.T) {
	r := &jsonMockRunner{json: `{"repository":"/repo","commit":"abc","schemaVersion":"1.6.0","scanComplete":true,"scanWarnings":["yaml: broken.yaml: parse error"],"scanCoverage":{"discovered":4,"parsed":3,"ignored":1}}`}
	meta := parseGraphMetadata(r)
	if !meta.Available || meta.Repository != "/repo" || meta.Commit != "abc" || meta.SchemaVersion != "1.6.0" || !meta.ScanComplete {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if meta.ScanCoverage == nil || meta.ScanCoverage.Discovered != 4 || meta.ScanCoverage.Ignored != 1 {
		t.Fatalf("unexpected scan coverage: %+v", meta.ScanCoverage)
	}
	if len(meta.ScanWarnings) != 1 || meta.ScanWarnings[0] != "yaml: broken.yaml: parse error" {
		t.Fatalf("unexpected scan warnings: %+v", meta.ScanWarnings)
	}
}

func TestParseCommitFromStats(t *testing.T) {
	r := &mockRunner{responses: map[string]string{
		"stats": "commit: abc123\nbranch: main\ngenerated: 2026-08-01\nentities: 100\n",
	}}
	got := parseCommitFromStats(r)
	if got != "abc123" {
		t.Errorf("expected abc123, got %q", got)
	}
}

func TestParseCommitFromStats_NoCommit(t *testing.T) {
	r := &mockRunner{responses: map[string]string{
		"stats": "entities: 100\nrelationships: 50\n",
	}}
	got := parseCommitFromStats(r)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestFreshnessWarning_Stale(t *testing.T) {
	f := Freshness{GraphCommit: "abc123def456", RepoHead: "xyz789000111", Stale: true, Available: true}
	w := f.Warning()
	if w == "" {
		t.Error("expected warning for stale graph")
	}
	if !strings.Contains(w, "abc123def4") {
		t.Errorf("warning should contain short graph commit, got: %s", w)
	}
}

func TestFreshnessWarning_Fresh(t *testing.T) {
	f := Freshness{
		GraphCommit:         "abc123",
		RepoHead:            "abc123",
		SchemaVersion:       CurrentSchemaVersion,
		SchemaCurrent:       true,
		ExtractorCurrent:    true,
		ExtractionSignature: "fixture",
		Stale:               false,
		Available:           true,
		Verifiable:          true,
		StateVerifiable:     true,
		EntityIdentity:      "repository-path-v1",
	}
	if f.Warning() != "" {
		t.Error("expected no warning for fresh graph")
	}
}

func TestFreshnessWarning_ReportsIgnoredCoverage(t *testing.T) {
	f := Freshness{
		GraphCommit:         "abc123",
		RepoHead:            "abc123",
		SchemaVersion:       CurrentSchemaVersion,
		SchemaCurrent:       true,
		ExtractorCurrent:    true,
		ExtractionSignature: "fixture",
		Available:           true,
		Verifiable:          true,
		StateVerifiable:     true,
		EntityIdentity:      "repository-path-v1",
		ScanCoverage:        &ScanCoverage{Discovered: 5, Parsed: 4, Ignored: 1},
	}
	if warning := f.Warning(); !strings.Contains(warning, "without a registered parser") {
		t.Fatalf("warning = %q, want ignored-file limitation", warning)
	}
}

func TestFreshnessWarning_Unavailable(t *testing.T) {
	f := Freshness{}
	if !strings.Contains(f.Warning(), "cannot be grounded") {
		t.Errorf("expected unavailable graph warning, got %q", f.Warning())
	}
	if !f.BlocksImplementation() {
		t.Error("unavailable graph must block implementation guidance")
	}
}

func TestFreshnessBlocksImplementationWhenUnverifiable(t *testing.T) {
	f := Freshness{Available: true, Incomplete: false, Verifiable: false}
	if !f.BlocksImplementation() {
		t.Error("unverifiable graph must block implementation guidance")
	}
}

func TestFreshnessPromptContextIncludesVerificationLimits(t *testing.T) {
	f := Freshness{
		GraphCommit:     "abc123456789",
		RepoHead:        "abc123456789",
		GraphRepository: "/repo",
		EntityIdentity:  "repository-path-v1",
		Incomplete:      true,
		StateVerifiable: false,
		Available:       true,
		Verifiable:      true,
	}
	got := f.PromptContext()
	for _, expected := range []string{
		"graph freshness: current",
		"repository match: matches requested checkout",
		"scan: incomplete",
		"repository state verification: unverified",
		"graph commit: abc1234567",
		"limitation: graph scan is incomplete",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("prompt context missing %q: %s", expected, got)
		}
	}
}

func TestParseFreshnessIncludesCoverage(t *testing.T) {
	r := &jsonMockRunner{json: `{"available":true,"graphRepository":"/repo","repository":"/repo","graphCommit":"abc","repoHead":"abc","schemaVersion":"1.6.0","schemaCurrent":true,"extractorCurrent":true,"extractionSignature":"fixture","entityIdentity":"repository-path-v1","scanComplete":true,"scanWarnings":["yaml: warning"],"repositoryMatch":true,"verifiable":true,"stale":false,"dirty":false,"stateVerifiable":true,"scanCoverage":{"discovered":8,"parsed":7,"reused":1}}`}
	f, ok := parseFreshness(r, "/repo")
	if !ok || f.SchemaVersion != CurrentSchemaVersion || !f.SchemaCurrent || f.ScanCoverage == nil || f.ScanCoverage.Discovered != 8 || f.ScanCoverage.Reused != 1 || len(f.ScanWarnings) != 1 {
		t.Fatalf("freshness = %+v, ok=%v", f, ok)
	}
}

func TestFreshnessPromptContextIncludesBoundedScanWarnings(t *testing.T) {
	f := Freshness{Available: true, ScanWarnings: []string{"warning-1", "warning-2", "warning-3", "warning-4", "warning-5", "warning-6"}}
	got := f.PromptContext()
	if !strings.Contains(got, "scan warning: warning-1") || !strings.Contains(got, "scan warning: warning-5") {
		t.Fatalf("prompt context omitted scan warnings: %s", got)
	}
	if !strings.Contains(got, "scan warnings: 1 additional warning(s) omitted") {
		t.Fatalf("prompt context omitted warning truncation: %s", got)
	}
	if strings.Contains(got, "scan warning: warning-6") {
		t.Fatalf("prompt context exceeded warning bound: %s", got)
	}
}

func TestShort(t *testing.T) {
	if short("abc") != "abc" {
		t.Error("short should return full string when < 10 chars")
	}
	if short("0123456789abcdef") != "0123456789" {
		t.Errorf("short should truncate to 10, got %q", short("0123456789abcdef"))
	}
}
