package workingset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type graphTestRunner struct {
	calls []string
}

func (r *graphTestRunner) Run(args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	return "", fmt.Errorf("unexpected Atlas call: %s", strings.Join(args, " "))
}

func (r *graphTestRunner) RunJSON(args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	return "", fmt.Errorf("unexpected Atlas call: %s", strings.Join(args, " "))
}

func (r *graphTestRunner) GraphPath() string { return "" }

func TestExtractFuncBlock(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		funcName string
		want     string
	}{
		{
			name:     "simple function",
			content:  "func Foo() { return }",
			funcName: "Foo",
			want:     "func Foo() { return }",
		},
		{
			name:     "function with preceding comments",
			content:  "// comment\nfunc Bar() { }",
			funcName: "Bar",
			want:     "// comment\nfunc Bar() { }",
		},
		{
			name: "multi-line with nested braces",
			content: `func Baz() {
	if x {
		y()
	}
}`,
			funcName: "Baz",
			want: `func Baz() {
	if x {
		y()
	}
}`,
		},
		{
			name:     "function not found",
			content:  "func Foo() { return }",
			funcName: "Missing",
			want:     "",
		},
		{
			name:     "method receiver",
			content:  "func (r *Reconciler) Reconcile(ctx context.Context) { return nil }",
			funcName: "Reconcile",
			want:     "func (r *Reconciler) Reconcile(ctx context.Context) { return nil }",
		},
		{
			name:     "substring false positive",
			content:  "func ReconcileAll() { return }\nfunc Reconcile(ctx context.Context) { return nil }",
			funcName: "Reconcile",
			want:     "func Reconcile(ctx context.Context) { return nil }",
		},
		{
			name: "multi-line signature no opening brace",
			content: `func LongFunc(
	a int,
	b int,
) error {
	return nil
}`,
			funcName: "LongFunc",
			// No '{' on func line → returns just the signature line
			want: "func LongFunc(",
		},
		{
			name: "multiple preceding comments",
			content: `package main

// First comment
// Second comment
func Foo() {
	bar()
}`,
			funcName: "Foo",
			want: `// First comment
// Second comment
func Foo() {
	bar()
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFuncBlock(tt.content, tt.funcName)
			if got != tt.want {
				t.Errorf("extractFuncBlock() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestExtractFunctions(t *testing.T) {
	tests := []struct {
		name      string
		atlasData string
		want      []string
	}{
		{
			name: "function entity pattern",
			atlasData: `function:hostedcluster.reconcileHostedControlPlane
function:hostedcluster.Reconcile`,
			want: []string{"reconcileHostedControlPlane", "Reconcile"},
		},
		{
			name:      "calls pattern extracts after last dot",
			atlasData: `Calls: reconcileEtcd, util.ReconcileWorkerConfig`,
			want:      []string{"reconcileEtcd", "ReconcileWorkerConfig"},
		},
		{
			name: "deduplication",
			atlasData: `function:hostedcluster.Reconcile
Calls: Reconcile, util.CreateConfig`,
			want: []string{"Reconcile", "CreateConfig"},
		},
		{
			name:      "short names filtered",
			atlasData: `function:pkg.Foo`,
			want:      nil,
		},
		{
			name: "realistic atlas output",
			atlasData: `function:hostedcluster.reconcileHostedControlPlane
Calls: reconcileEtcd, util.ReconcileWorkerConfig
function:hostedcluster.Reconcile`,
			want: []string{"reconcileHostedControlPlane", "Reconcile", "reconcileEtcd", "ReconcileWorkerConfig"},
		},
		{
			name:      "empty input",
			atlasData: "",
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFunctions(tt.atlasData)
			if len(got) != len(tt.want) {
				t.Fatalf("extractFunctions() returned %d items %v, want %d items %v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("extractFunctions()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractFunctionIDsUsesStructuredEntityIdentity(t *testing.T) {
	data := `{"entities":[{"id":"function:github.com/example/repo/pkg.Reconcile","name":"Reconcile","kind":"function"},{"id":"function:github.com/example/repo/pkg.Helper","name":"Helper","kind":"function"}]}`
	got := extractFunctionIDs(data)
	if len(got) != 2 || got[0] != "function:github.com/example/repo/pkg.Helper" || got[1] != "function:github.com/example/repo/pkg.Reconcile" {
		t.Fatalf("extractFunctionIDs() = %v, want exact graph IDs", got)
	}
}

func TestLimitWorkingSetPreservesBudget(t *testing.T) {
	ws := &WorkingSet{
		Types:     "types\n" + strings.Repeat("t", 200),
		ImplFiles: []FileContent{{Path: "controller.go", Code: strings.Repeat("i", 200)}},
		TestFiles: []FileContent{{Path: "controller_test.go", Code: strings.Repeat("x", 200)}},
	}
	limitWorkingSet(ws, 100)
	if ws.TotalChars() > 100 {
		t.Fatalf("working set has %d chars, want <= 100", ws.TotalChars())
	}
}

func TestBuildReadsGraphSelectedFunctionsAcrossFiles(t *testing.T) {
	repo := t.TempDir()
	for name, code := range map[string]string{
		"controllers/setup.go":     "package controllers\n\nfunc Setup() {}\n",
		"controllers/reconcile.go": "package controllers\n\nfunc Reconcile() {\n\tcallHelper()\n}\n",
		"controllers/helper.go":    "package controllers\n\nfunc Helper() {\n\treturn\n}\n",
	} {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(code), 0644); err != nil {
			t.Fatal(err)
		}
	}

	atlasData := `{"entities":[
{"id":"controller:example.com/repo/controllers.Reconciler","name":"Reconciler","kind":"controller","source":{"file":"controllers/setup.go","line":3}},
{"id":"function:example.com/repo/controllers.Reconcile","name":"Reconcile","kind":"function","source":{"file":"controllers/reconcile.go","line":3,"endLine":5}},
{"id":"function:example.com/repo/controllers.Helper","name":"Helper","kind":"function","source":{"file":"controllers/helper.go","line":3,"endLine":5}}
]}`

	ws := Build(repo, atlasData, "", "controllers/setup.go")
	if len(ws.ImplFiles) < 3 {
		t.Fatalf("expected controller context plus exact functions from multiple files, got %+v", ws.ImplFiles)
	}
	joined := ""
	for _, file := range ws.ImplFiles {
		joined += file.Path + "\n" + file.Code + "\n"
	}
	for _, want := range []string{"controllers/setup.go", "controllers/reconcile.go → Reconcile()", "controllers/helper.go → Helper()", "callHelper()"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("working set missing %q:\n%s", want, joined)
		}
	}
}

func TestBuildForControllerKeepsOnlyEvidencedCallTargets(t *testing.T) {
	repo := t.TempDir()
	for name, code := range map[string]string{
		"controllers/setup.go":     "package controllers\n\nfunc Setup() {}\n",
		"controllers/reconcile.go": "package controllers\n\nfunc Reconcile() {\n\tcallHelper()\n}\n",
		"controllers/unrelated.go": "package controllers\n\nfunc Unrelated() {\n\tpanic(\"should not be selected\")\n}\n",
	} {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(code), 0644); err != nil {
			t.Fatal(err)
		}
	}

	atlasData := `{"entity":{"id":"controller:example.com/repo/controllers.Reconciler","name":"Reconciler","kind":"controller","source":{"file":"controllers/setup.go","line":3}},
"outgoing":{"calls":[{"relationship":{"id":"controller:example.com/repo/controllers.Reconciler--calls--function:example.com/repo/controllers.Reconcile","from":"controller:example.com/repo/controllers.Reconciler","to":"function:example.com/repo/controllers.Reconcile","type":"calls","confidence":"inferred","evidence":{"file":"controllers/setup.go","line":3,"reason":"call"}},"target":{"id":"function:example.com/repo/controllers.Reconcile","name":"Reconcile","kind":"function","source":{"file":"controllers/reconcile.go","line":3,"endLine":5}}}]},
"siblings":[{"id":"function:example.com/repo/controllers.Unrelated","name":"Unrelated","kind":"function","source":{"file":"controllers/unrelated.go","line":3,"endLine":5}}]}`

	ws := BuildForController(
		repo,
		atlasData,
		"",
		"controller:example.com/repo/controllers.Reconciler",
		"controllers/setup.go",
	)

	if len(ws.Functions) != 1 || ws.Functions[0] != "function:example.com/repo/controllers.Reconcile" {
		t.Fatalf("working set selected unrelated functions: %v", ws.Functions)
	}
	joined := ""
	for _, file := range ws.ImplFiles {
		joined += file.Path + "\n" + file.Code + "\n"
	}
	if !strings.Contains(joined, "controllers/reconcile.go → Reconcile()") {
		t.Fatalf("working set omitted evidenced call target:\n%s", joined)
	}
	if strings.Contains(joined, "should not be selected") || strings.Contains(joined, "Unrelated()") {
		t.Fatalf("working set included sibling function:\n%s", joined)
	}
}

func TestBuildForControllerIncludesContainedImplementationMethods(t *testing.T) {
	repo := t.TempDir()
	for name, code := range map[string]string{
		"controllers/setup.go":     "package controllers\n\nfunc (r *Reconciler) SetupWithManager() {}\n",
		"controllers/reconcile.go": "package controllers\n\nfunc (r *Reconciler) Reconcile() {\n\thandle()\n}\n",
		"controllers/unrelated.go": "package controllers\n\nfunc Unrelated() { panic(\"should not be selected\") }\n",
	} {
		path := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(code), 0644); err != nil {
			t.Fatal(err)
		}
	}

	controllerID := "controller:example.com/repo/controllers.Reconciler"
	reconcileID := "function:example.com/repo/controllers.Reconciler.Reconcile"
	setupID := "function:example.com/repo/controllers.Reconciler.SetupWithManager"
	atlasData := fmt.Sprintf(`{"entities":[
{"id":%q,"name":"Reconciler","kind":"controller","source":{"file":"controllers/setup.go","line":3}},
{"id":%q,"name":"Reconcile","kind":"function","source":{"file":"controllers/reconcile.go","line":3,"endLine":5}},
{"id":%q,"name":"SetupWithManager","kind":"function","source":{"file":"controllers/setup.go","line":3,"endLine":3}},
{"id":"function:example.com/repo/controllers.Unrelated","name":"Unrelated","kind":"function","source":{"file":"controllers/unrelated.go","line":3,"endLine":3}}],
"relationships":[
{"id":%q,"from":%q,"to":%q,"type":"contains","confidence":"proven","evidence":{"file":"controllers/reconcile.go","line":3,"reason":"controller Reconcile method"}},
{"id":%q,"from":%q,"to":%q,"type":"contains","confidence":"proven","evidence":{"file":"controllers/setup.go","line":3,"reason":"controller setup method"}}]}`,
		controllerID,
		reconcileID,
		setupID,
		controllerID+"--contains--"+reconcileID, controllerID, reconcileID,
		controllerID+"--contains--"+setupID, controllerID, setupID,
	)

	ws := BuildForController(repo, atlasData, "", controllerID, "controllers/setup.go")
	if len(ws.Functions) != 2 || ws.Functions[0] != reconcileID || ws.Functions[1] != setupID {
		t.Fatalf("working set omitted contained controller methods: %v", ws.Functions)
	}
	joined := ""
	for _, file := range ws.ImplFiles {
		joined += file.Path + "\n" + file.Code + "\n"
	}
	for _, want := range []string{"controllers/reconcile.go → Reconcile()", "handle()", "controllers/setup.go → SetupWithManager()"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("working set missing contained implementation method %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "should not be selected") || strings.Contains(joined, "Unrelated()") {
		t.Fatalf("working set included unrelated sibling function:\n%s", joined)
	}
}

func TestFindTestsReusesExistingGraphTestRelationship(t *testing.T) {
	repo := t.TempDir()
	testPath := filepath.Join(repo, "controllers", "reconcile_test.go")
	if err := os.MkdirAll(filepath.Dir(testPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, []byte("package controllers\n\nfunc TestReconcile() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	controllerID := "controller:example.com/repo/controllers.Reconciler"
	functionID := "function:example.com/repo/controllers.Reconcile"
	testID := "test:example.com/repo/controllers.TestReconcile"
	atlasData := fmt.Sprintf(`{"entities":[{"id":%q,"kind":"controller","source":{"file":"controllers/reconcile.go"}},{"id":%q,"kind":"function","source":{"file":"controllers/reconcile.go"}},{"id":%q,"kind":"test","source":{"file":"controllers/reconcile_test.go"}}],"relationships":[{"id":"%s--tested_by--%s","from":%q,"to":%q,"type":"tested_by","confidence":"inferred","evidence":{"parser":"go-ast","file":"controllers/reconcile_test.go","line":3,"reason":"test body directly invokes function; invocation does not prove assertions or behavior coverage"}}]}`,
		controllerID, functionID, testID, functionID, testID, functionID, testID)
	runner := &graphTestRunner{}
	files := findTestsFromGraph(runner, repo, controllerID, []string{functionID}, atlasData)
	if len(files) != 1 || files[0].Path != "controllers/reconcile_test.go" {
		t.Fatalf("unexpected graph-selected test files: %+v", files)
	}
	for _, want := range []string{"tested_by (inferred)", "controllers/reconcile_test.go:3", "does not prove assertions or behavior coverage"} {
		if !strings.Contains(files[0].Evidence, want) {
			t.Errorf("test relationship evidence missing %q: %s", want, files[0].Evidence)
		}
	}
	if len(runner.calls) != 0 {
		t.Fatalf("existing test relationship caused redundant Atlas calls: %v", runner.calls)
	}
}

func TestAppendGraphTestPathsRequiresSelectedFunctionAndEvidence(t *testing.T) {
	functionID := "function:example.com/repo/controllers.Reconcile"
	unrelatedID := "function:example.com/repo/controllers.Other"
	testID := "test:example.com/repo/controllers.TestReconcile"
	data := fmt.Sprintf(`{"entities":[{"id":%q,"kind":"function"},{"id":%q,"kind":"function"},{"id":%q,"kind":"test","source":{"file":"controllers/reconcile_test.go"}}],"relationships":[
{"id":"%s--verifies--%s","from":%q,"to":%q,"type":"verifies","confidence":"proven","evidence":{"file":"controllers/reconcile_test.go","line":3,"reason":"not a supported test link"}},
{"id":"%s--tested_by--%s","from":%q,"to":%q,"type":"tested_by","confidence":"inferred","evidence":{"file":"controllers/reconcile_test.go","line":3,"reason":"belongs to another function"}},
{"id":"%s--tested_by--%s","from":%q,"to":%q,"type":"tested_by","confidence":"inferred","evidence":{"file":"controllers/reconcile_test.go","line":0,"reason":"missing source location"}}]}`,
		functionID, unrelatedID, testID,
		functionID, testID, functionID, testID,
		unrelatedID, testID, unrelatedID, testID,
		functionID, testID, functionID, testID)
	var paths []string
	evidenceByPath := make(map[string][]string)
	appendGraphTestPaths(data, []string{functionID}, make(map[string]bool), &paths, evidenceByPath)
	if len(paths) != 0 || len(evidenceByPath) != 0 {
		t.Fatalf("unsupported or ungrounded links selected a test: paths=%v evidence=%v", paths, evidenceByPath)
	}
}
