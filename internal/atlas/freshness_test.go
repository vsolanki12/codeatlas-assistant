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
	r := &jsonMockRunner{json: `{"repository":"/repo","commit":"abc","scanComplete":true}`}
	meta := parseGraphMetadata(r)
	if !meta.Available || meta.Repository != "/repo" || meta.Commit != "abc" || !meta.ScanComplete {
		t.Fatalf("unexpected metadata: %+v", meta)
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
		GraphCommit:     "abc123",
		RepoHead:        "abc123",
		Stale:           false,
		Available:       true,
		Verifiable:      true,
		StateVerifiable: true,
		EntityIdentity:  "repository-path-v1",
	}
	if f.Warning() != "" {
		t.Error("expected no warning for fresh graph")
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

func TestShort(t *testing.T) {
	if short("abc") != "abc" {
		t.Error("short should return full string when < 10 chars")
	}
	if short("0123456789abcdef") != "0123456789" {
		t.Errorf("short should truncate to 10, got %q", short("0123456789abcdef"))
	}
}
