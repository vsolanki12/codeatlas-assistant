package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type taskRunner struct {
	repo    string
	packets map[string]string
}

func (r *taskRunner) GraphPath() string                  { return "" }
func (r *taskRunner) Run(args ...string) (string, error) { return r.RunJSON(args...) }
func (r *taskRunner) RunJSON(args ...string) (string, error) {
	switch args[0] {
	case "stats":
		return fmt.Sprintf(`{"repository":%q,"schemaVersion":"1.6.0","commit":"fixture","scanComplete":true}`, r.repo), nil
	case "freshness":
		return `{"available":true,"schemaVersion":"1.6.0","schemaCurrent":true,"extractorCurrent":true,"extractionSignature":"fixture","graphCommit":"fixture","repoHead":"fixture","entityIdentity":"repository-path-v1","scanComplete":true,"repositoryMatch":true,"verifiable":true,"stateVerifiable":true}`, nil
	case "evidence":
		key := strings.Join(args, " ")
		data, ok := r.packets[key]
		if !ok {
			return "", fmt.Errorf("unexpected task call %s", key)
		}
		return data, nil
	default:
		return "", fmt.Errorf("unexpected query %s", args[0])
	}
}

func TestEvaluationFailsSmallerPacketMissingRequiredTestEvidence(t *testing.T) {
	repo := t.TempDir()
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\nfunc Reconcile() {}\n"), 0644)
	_ = os.WriteFile(filepath.Join(repo, "a_test.go"), []byte("package a\nfunc TestReconcile() {}\n"), 0644)
	base := fmt.Sprintf(`{"version":"1.0","status":"ok","graphFingerprint":"fixture","graph":{"repository":%q},"entities":[{"id":"function:a","kind":"function"}],"sources":[{"entityID":"function:a","role":"implementation","source":{"file":"a.go","line":2,"endLine":2}}]}`, repo)
	complete := fmt.Sprintf(`{"version":"1.0","status":"ok","graphFingerprint":"fixture","graph":{"repository":%q},"entities":[{"id":"function:a","kind":"function"},{"id":"test:a","kind":"test"}],"sources":[{"entityID":"function:a","role":"implementation","source":{"file":"a.go","line":2,"endLine":2}},{"entityID":"test:a","role":"test","source":{"file":"a_test.go","line":2,"endLine":2}}]}`, repo)
	runner := &taskRunner{repo: repo, packets: map[string]string{
		"evidence --question AutoRepair NodePool --intent understand --budget-bytes 2048":  base,
		"evidence --question AutoRepair NodePool --intent understand --budget-bytes 16384": complete,
	}}
	tasks := []Task{{ID: "autorepair", Question: "AutoRepair NodePool", RequiredEntities: []string{"function:a"}, RequiredSources: []SourceRequirement{{File: "a_test.go", Role: "test", Line: 2}}, RequiredSnippets: []SnippetRequirement{{Contains: "TestReconcile"}}}}
	results, err := Evaluate(runner, tasks, []int{2048, 16384}, repo)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Passed || !results[1].Passed {
		t.Fatalf("smaller but incomplete evidence did not fail: %+v", results)
	}
	if results[1].UniqueFiles != 2 || results[1].UniqueSourceLines != 2 || results[1].Prompt.EstimatedTokens == 0 {
		t.Fatalf("end-to-end retrieval accounting missing: %+v", results[1])
	}
}

func TestEvaluationSupportsExpectedNoMatchAndRequiresCorrectness(t *testing.T) {
	runner := &taskRunner{repo: t.TempDir(), packets: map[string]string{"evidence --question nonexistent --intent understand --budget-bytes 2048": `{"version":"1.0","status":"no_match","graphFingerprint":"fixture"}`}}
	results, err := Evaluate(runner, []Task{{ID: "negative", Question: "nonexistent", ExpectedStatus: "no_match"}}, []int{2048}, runner.repo)
	if err != nil || len(results) != 1 || !results[0].Passed {
		t.Fatalf("negative retrieval result not validated: %+v error=%v", results, err)
	}
	if _, err := Evaluate(runner, []Task{{ID: "meaningless", Question: "anything"}}, []int{2048}, runner.repo); err == nil {
		t.Fatal("size-only fixture accepted as correctness evaluation")
	}
}

func TestEvaluationRejectsSnippetOnlyPresentInSelectionMetadata(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\nfunc Reconcile() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "types.go"), []byte("package a\nAutoRepair bool\n"), 0644); err != nil {
		t.Fatal(err)
	}
	packet := fmt.Sprintf(`{"version":"1.0","status":"ok","graphFingerprint":"fixture","graph":{"repository":%q},"entities":[{"id":"function:a","kind":"function"},{"id":"field:a","kind":"field"}],"sources":[{"entityID":"function:a","role":"implementation","source":{"file":"a.go","line":2,"endLine":2},"reason":"if nodePool.Management.AutoRepair {"},{"entityID":"field:a","role":"definition","source":{"file":"types.go","line":2,"endLine":2},"reason":"type declaration"}]}`, repo)
	runner := &taskRunner{repo: repo, packets: map[string]string{"evidence --question AutoRepair NodePool --intent understand --budget-bytes 16384": packet}}
	tasks := []Task{
		{ID: "metadata_only", Question: "AutoRepair NodePool", RequiredSnippets: []SnippetRequirement{{Contains: "if nodePool.Management.AutoRepair {"}}},
		{ID: "header_only", Question: "AutoRepair NodePool", RequiredSnippets: []SnippetRequirement{{Contains: "### a.go"}}},
		{ID: "retained_definition", Question: "AutoRepair NodePool", RequiredSnippets: []SnippetRequirement{{Contains: "AutoRepair bool"}}},
		{ID: "retained_implementation", Question: "AutoRepair NodePool", RequiredSnippets: []SnippetRequirement{{Contains: "func Reconcile() {}"}}},
	}
	results, err := Evaluate(runner, tasks, []int{16384}, repo)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Passed || results[1].Passed || !results[2].Passed || !results[3].Passed {
		t.Fatalf("metadata and retained source were not distinguished: %+v", results)
	}
}
