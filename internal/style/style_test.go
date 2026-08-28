package style

import "testing"

func TestExtractGoFilePathFromStructuredAtlasJSON(t *testing.T) {
	data := `{"entities":[{"id":"function:example.com/repo/pkg.Reconcile","name":"Reconcile","kind":"function","source":{"file":"pkg/reconcile.go","line":12}}]}`
	if got := extractGoFilePath(data); got != "pkg/reconcile.go" {
		t.Fatalf("extractGoFilePath() = %q, want pkg/reconcile.go", got)
	}
}

func TestExtractGoFilePathSkipsTestsFromStructuredAtlasJSON(t *testing.T) {
	data := `{"entities":[{"id":"test:example.com/repo/pkg.TestReconcile","name":"TestReconcile","kind":"test","source":{"file":"pkg/reconcile_test.go","line":12},"files":["pkg/reconcile_test.go"]},{"id":"function:example.com/repo/pkg.Reconcile","name":"Reconcile","kind":"function","source":{"file":"pkg/reconcile.go","line":12}}]}`
	if got := extractGoFilePath(data); got != "pkg/reconcile.go" {
		t.Fatalf("extractGoFilePath() = %q, want pkg/reconcile.go", got)
	}
}

func TestExtractGoFilePathSkipsNonGoSources(t *testing.T) {
	data := `{"entities":[{"id":"crd:example.io.widget","name":"Widget","kind":"crd","source":{"file":"config/crd.yaml","line":1}},{"id":"document:docs/design.md","name":"design.md","kind":"document","source":{"file":"docs/design.md","line":1}},{"id":"function:example.com/repo/pkg.Reconcile","name":"Reconcile","kind":"function","source":{"file":"pkg/reconcile.go","line":12}}]}`
	if got := extractGoFilePath(data); got != "pkg/reconcile.go" {
		t.Fatalf("extractGoFilePath() = %q, want pkg/reconcile.go", got)
	}
}
