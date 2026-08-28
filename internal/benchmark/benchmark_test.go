package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type mockRunner struct {
	responses map[string]string
	calls     []string
}

func (m *mockRunner) Run(args ...string) (string, error) {
	return "", fmt.Errorf("unexpected text Atlas call: %s", strings.Join(args, " "))
}

func (m *mockRunner) RunJSON(args ...string) (string, error) {
	key := strings.Join(args, " ")
	m.calls = append(m.calls, key)
	result, ok := m.responses[key]
	if !ok {
		return "", fmt.Errorf("unexpected Atlas call: %s", key)
	}
	return result, nil
}

func (m *mockRunner) GraphPath() string { return "graph.json" }

func TestRunMeasuresCompactContextAgainstFullAndGraphSelectedSource(t *testing.T) {
	repo := t.TempDir()
	sourcePath := filepath.Join(repo, "controllers", "widget.go")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("package controllers\n\nfunc Reconcile() {}\n"+strings.Repeat("// repository source context\n", 300)), 0644); err != nil {
		t.Fatal(err)
	}

	entityID := "controller:example.com/repo/controllers.WidgetReconciler"
	full := fmt.Sprintf(`{"graph":{"repository":%q},"entity":{"id":%q,"name":"WidgetReconciler","kind":"controller","source":{"file":"controllers/widget.go","line":3}},"relationships":[{"id":"%s--creates--resource:apps/Deployment","from":%q,"to":"resource:apps/Deployment","type":"creates","confidence":"proven","evidence":{"file":"controllers/widget.go","line":3}}],"detail":"%s"}`,
		repo, entityID, entityID, entityID, strings.Repeat("full context ", 100))
	compact := fmt.Sprintf(`{"graph":{"repository":%q},"entity":{"id":%q,"kind":"controller"},"relationships":[]}`,
		repo, entityID)
	runner := &mockRunner{responses: map[string]string{
		"ask " + entityID + " --intent debug":           full,
		"ask " + entityID + " --intent debug --compact": compact,
	}}

	result, err := Run(runner, entityID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("benchmark made %d Atlas calls, want 2: %v", len(runner.calls), runner.calls)
	}
	if result.RawRepository == nil || result.RawRepository.Bytes == 0 {
		t.Fatalf("expected graph-selected source measurement: %+v", result)
	}
	if len(result.Files) != 1 || result.Files[0] != "controllers/widget.go" {
		t.Fatalf("unexpected selected source files: %v", result.Files)
	}
	if result.CompactVsFullReductionPercent <= 0 {
		t.Fatalf("compact prompt should be smaller than full prompt: %+v", result)
	}
	if result.RawPrompt == nil || result.CompactVsRawReductionPercent <= 0 {
		t.Fatalf("expected compact reduction against raw source baseline: %+v", result)
	}
	if !strings.Contains(result.Format(), "compact vs full prompt reduction") {
		t.Fatal("formatted benchmark omitted reduction report")
	}
}

func TestRunRequiresEntity(t *testing.T) {
	if _, err := Run(&mockRunner{}, "", ""); err == nil {
		t.Fatal("empty entity should be rejected")
	}
}
