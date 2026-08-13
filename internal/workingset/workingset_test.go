package workingset

import (
	"testing"

	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
)

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

func TestCommonPathPrefix(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want string
	}{
		{"shared prefix", "a/b/c", "a/b/d", "a/b"},
		{"no common prefix", "x/y", "a/b", ""},
		{"one is prefix of other", "a/b/c", "a/b/c/d", "a/b/c"},
		{"empty string", "", "a", ""},
		{"identical paths", "a/b/c", "a/b/c", "a/b/c"},
		{"single segment match", "a/x", "a/y", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commonPathPrefix(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("commonPathPrefix(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestExtractJIRAComponents(t *testing.T) {
	framework := &prompt.FrameworkInfo{
		Components: "kube_apiserver/ | Deployment | deployment.go\netcd/ | StatefulSet | statefulset.go",
	}

	tests := []struct {
		name     string
		jiraText string
		want     map[string]bool
	}{
		{
			name:     "matches hyphen variant",
			jiraText: "fix kube-apiserver deployment",
			want:     map[string]bool{"kube_apiserver": true},
		},
		{
			name:     "matches underscore variant",
			jiraText: "fix kube_apiserver deployment",
			want:     map[string]bool{"kube_apiserver": true},
		},
		{
			name:     "matches space variant",
			jiraText: "fix kube apiserver deployment",
			want:     map[string]bool{"kube_apiserver": true},
		},
		{
			name:     "no matches",
			jiraText: "nothing relevant here",
			want:     map[string]bool{},
		},
		{
			name:     "matches etcd",
			jiraText: "etcd backup failing on upgrade",
			want:     map[string]bool{"etcd": true},
		},
		{
			name:     "matches multiple components",
			jiraText: "kube-apiserver and etcd both failing",
			want:     map[string]bool{"kube_apiserver": true, "etcd": true},
		},
		{
			name:     "case insensitive",
			jiraText: "ETCD backup failing",
			want:     map[string]bool{"etcd": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJIRAComponents(tt.jiraText, framework)
			if len(got) != len(tt.want) {
				t.Fatalf("extractJIRAComponents() = %v, want %v", got, tt.want)
			}
			for k := range tt.want {
				if !got[k] {
					t.Errorf("extractJIRAComponents() missing key %q", k)
				}
			}
		})
	}
}

func TestExtractReferencedComponents(t *testing.T) {
	tests := []struct {
		name          string
		atlasData     string
		frameworkPath string
		wantContains  []string
	}{
		{
			name:          "file path extraction",
			atlasData:     "control-plane-operator/hostedclusterconfigoperator/controllers/resources/kube_apiserver/deployment.go:42",
			frameworkPath: "control-plane-operator/hostedclusterconfigoperator/controllers/resources",
			wantContains:  []string{"kube_apiserver"},
		},
		{
			name:          "package pattern extraction",
			atlasData:     "function:hostedcluster.Reconcile",
			frameworkPath: "some/path",
			wantContains:  []string{"hostedcluster"},
		},
		{
			name:          "empty atlas data",
			atlasData:     "",
			frameworkPath: "some/path",
			wantContains:  nil,
		},
		{
			name:          "both patterns match",
			atlasData:     "function:etcd.Reconcile\ncontrol-plane-operator/hostedclusterconfigoperator/controllers/resources/kube_apiserver/deployment.go:42",
			frameworkPath: "control-plane-operator/hostedclusterconfigoperator/controllers/resources",
			wantContains:  []string{"kube_apiserver", "etcd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractReferencedComponents(tt.atlasData, tt.frameworkPath)
			for _, k := range tt.wantContains {
				if !got[k] {
					t.Errorf("extractReferencedComponents() missing key %q, got %v", k, got)
				}
			}
		})
	}
}
