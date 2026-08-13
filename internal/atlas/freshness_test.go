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
	f := Freshness{GraphCommit: "abc123def456", RepoHead: "xyz789000111", Stale: true}
	w := f.Warning()
	if w == "" {
		t.Error("expected warning for stale graph")
	}
	if !strings.Contains(w, "abc123def4") {
		t.Errorf("warning should contain short graph commit, got: %s", w)
	}
}

func TestFreshnessWarning_Fresh(t *testing.T) {
	f := Freshness{GraphCommit: "abc123", RepoHead: "abc123", Stale: false}
	if f.Warning() != "" {
		t.Error("expected no warning for fresh graph")
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
