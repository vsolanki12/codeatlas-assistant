package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vsolanki12/codeatlas-assistant/internal/claude"
	"github.com/vsolanki12/codeatlas-assistant/internal/generate"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/solve"
)

type questionRunner struct {
	repo, packet string
	calls        [][]string
}

func (r *questionRunner) GraphPath() string                  { return "" }
func (r *questionRunner) Run(args ...string) (string, error) { return r.RunJSON(args...) }
func (r *questionRunner) RunJSON(args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	switch args[0] {
	case "stats":
		return fmt.Sprintf(`{"repository":%q,"schemaVersion":"1.6.0","commit":"fixture","scanComplete":true}`, r.repo), nil
	case "freshness":
		return fmt.Sprintf(`{"available":true,"graphRepository":%q,"schemaVersion":"1.6.0","schemaCurrent":true,"extractorCurrent":true,"extractionSignature":"fixture","graphCommit":"fixture","repoHead":"fixture","entityIdentity":"repository-path-v1","scanComplete":true,"repositoryMatch":true,"verifiable":true,"stateVerifiable":true}`, r.repo), nil
	case "evidence":
		return r.packet, nil
	case "where", "ask":
		var packet struct {
			Entities []struct {
				ID   string `json:"id"`
				Kind string `json:"kind"`
			} `json:"entities"`
			Sources []struct {
				EntityID string `json:"entityID"`
				Source   struct {
					File string `json:"file"`
				} `json:"source"`
			} `json:"sources"`
		}
		if err := json.Unmarshal([]byte(r.packet), &packet); err != nil {
			return "", err
		}
		if len(args) < 2 {
			return "", fmt.Errorf("missing lookup")
		}
		if args[0] == "ask" {
			for _, entity := range packet.Entities {
				if entity.ID == args[1] {
					return fmt.Sprintf(`{"entity":{"id":%q,"kind":%q}}`, entity.ID, entity.Kind), nil
				}
			}
		} else {
			for _, source := range packet.Sources {
				if source.Source.File == args[1] {
					return fmt.Sprintf(`{"entities":[{"id":%q,"source":{"file":%q}}]}`, source.EntityID, source.Source.File), nil
				}
			}
		}
		return `{"entities":[]}`, nil
	default:
		return "", fmt.Errorf("unexpected query %v", args)
	}
}

type recordingLLM struct {
	calls    int
	prompt   string
	response string
	err      error
}

func (l *recordingLLM) Generate(p string) error { l.calls++; l.prompt = p; return nil }
func (l *recordingLLM) GenerateString(p string) (string, error) {
	l.calls++
	l.prompt = p
	if l.err != nil {
		return "", l.err
	}
	if l.response != "" {
		return l.response, nil
	}
	return "grounded response", nil
}

func captureAssistantOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWrite, stderrWrite
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		stdoutWrite.Close()
		stderrWrite.Close()
		stdoutRead.Close()
		stderrRead.Close()
	}()
	stdoutResult, stderrResult := make(chan string, 1), make(chan string, 1)
	go func() { data, _ := io.ReadAll(stdoutRead); stdoutResult <- string(data) }()
	go func() { data, _ := io.ReadAll(stderrRead); stderrResult <- string(data) }()
	fn()
	stdoutWrite.Close()
	stderrWrite.Close()
	return <-stdoutResult, <-stderrResult
}

func usableQuestionRunner(t *testing.T) *questionRunner {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\nfunc Reconcile() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	id := "function:github.com/example/repo/pkg.CAPI.Reconcile"
	field := "field:github.com/example/repo/api.NodePoolManagement.AutoRepair"
	packet := fmt.Sprintf(`{"version":"1.0","status":"ok","graphFingerprint":"fixture","graph":{"repository":%q},"entities":[{"id":%q,"kind":"function"},{"id":%q,"kind":"field"}],"sources":[{"entityID":%q,"role":"implementation","source":{"file":"a.go","line":2,"endLine":2},"reason":"declaration"}]}`, repo, id, field, id)
	return &questionRunner{repo: repo, packet: packet}
}

func TestREPLSupportsDeferredAndCustomModelsWithoutDiscovery(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("exit\n"); err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()
	for _, custom := range []bool{false, true} {
		if _, err := input.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		os.Stdin = input
		calls := 0
		var llm ollama.LLM = &deferredModel{client: &ollama.Client{}, resolve: func(string, bool) (string, error) { calls++; return "", fmt.Errorf("offline") }}
		if custom {
			llm = &recordingLLM{}
		}
		out, _ := captureAssistantOutput(t, func() { runREPL(&questionRunner{}, llm, "", "") })
		if calls != 0 || !strings.Contains(out, "CodeAtlas Assistant") {
			t.Fatalf("REPL performed model discovery or failed: %q", out)
		}
		if !custom && !strings.Contains(out, "selected after retrieval") {
			t.Fatalf("missing deferred model label: %q", out)
		}
	}
}

func TestQuestionWithholdsUnverifiedAndEmptyOutput(t *testing.T) {
	runner := usableQuestionRunner(t)
	llm := &recordingLLM{response: "UNPUBLISHED MODEL BODY: edit `missing.go` and field:github.com/example/repo/api.Missing.AutoRepair"}
	var failure error
	out, diagnostic := captureAssistantOutput(t, func() { failure = handleQuestion(runner, llm, "How does AutoRepair work?", "") })
	if failure == nil || out != "" || !strings.Contains(diagnostic, "answer withheld") || !strings.Contains(diagnostic, "missing.go") || !strings.Contains(diagnostic, "[entity]") {
		t.Fatalf("invalid generated output escaped validation: out=%q diagnostic=%q error=%v", out, diagnostic, failure)
	}
	out, diagnostic = captureAssistantOutput(t, func() { failure = publishGeneratedOutput("answer", " \n ", runner.repo, runner) })
	if failure == nil || out != "" || !strings.Contains(diagnostic, "no content") {
		t.Fatal("empty model output was presented as success")
	}
}

func TestQuestionPresentsValidatedReferences(t *testing.T) {
	runner := usableQuestionRunner(t)
	response := "Use `a.go` and field:github.com/example/repo/api.NodePoolManagement.AutoRepair."
	llm := &recordingLLM{response: response}
	var failure error
	out, diagnostic := captureAssistantOutput(t, func() { failure = handleQuestion(runner, llm, "How does AutoRepair work?", "") })
	if failure != nil || out != response || strings.Contains(diagnostic, "withheld") {
		t.Fatalf("verified output was not presented: out=%q diagnostic=%q error=%v", out, diagnostic, failure)
	}
}

func TestREPLContinuesAfterModelFailure(t *testing.T) {
	runner := usableQuestionRunner(t)
	input, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("How does AutoRepair work?\nexit\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = oldStdin }()
	llm := &recordingLLM{err: fmt.Errorf("offline fixture")}
	out, diagnostic := captureAssistantOutput(t, func() { runREPL(runner, llm, "", runner.repo) })
	if llm.calls != 1 || strings.Count(out, "> ") < 2 || !strings.Contains(diagnostic, "offline fixture") {
		t.Fatalf("REPL did not recover: out=%q diagnostic=%q", out, diagnostic)
	}
}

func TestContextBudgetFlagsRejectUnsafeValues(t *testing.T) {
	for _, budget := range []struct{ evidence, source int }{{2047, 24000}, {1048577, 24000}, {16384, 0}, {16384, -1}, {16384, 1048577}} {
		if validateContextBudgets(budget.evidence, budget.source) == nil {
			t.Fatalf("unsafe budget accepted: %+v", budget)
		}
	}
	if err := validateContextBudgets(2048, 1); err != nil {
		t.Fatal(err)
	}
}

func TestQuestionNeverCallsModelForUnusableEvidence(t *testing.T) {
	for _, data := range []string{
		`{"version":"1.0","status":"no_match","graphFingerprint":"fixture"}`,
		`{"version":"1.0","status":"ambiguous","graphFingerprint":"fixture"}`,
		`{"version":"1.0","status":"budget_exhausted","graphFingerprint":"fixture"}`,
		`{"version":"2.0","status":"ok","graphFingerprint":"fixture"}`,
		`{"version":"1.0","status":"ok","graphFingerprint":"fixture","entities":[{"id":"function:a","kind":"function"}],"sources":[{"entityID":"function:a","role":"implementation","source":{"file":"missing.go","line":1,"endLine":2}}]}`,
	} {
		runner := &questionRunner{repo: t.TempDir(), packet: data}
		llm := &recordingLLM{}
		handleQuestion(runner, llm, "How does autoRepair work for NodePool?", runner.repo)
		if llm.calls != 0 {
			t.Fatalf("model called for unusable evidence %s", data)
		}
	}
}

func TestImplementationModesNeverCallModelForNoMatch(t *testing.T) {
	for _, mode := range []string{"solve", "generate", "claude"} {
		t.Run(mode, func(t *testing.T) {
			runner := &questionRunner{repo: t.TempDir(), packet: `{"version":"1.0","status":"no_match","graphFingerprint":"fixture"}`}
			llm := &recordingLLM{}
			switch mode {
			case "solve":
				solve.Run(runner, llm, "unknown concept", "", true, runner.repo)
			case "generate":
				generate.Run(runner, llm, "unknown concept", "", "", runner.repo)
			case "claude":
				claude.Run(runner, llm, "unknown concept", "", filepath.Join(runner.repo, "prompt.xml"), runner.repo, true)
			}
			if llm.calls != 0 {
				t.Fatalf("%s called model with no evidence", mode)
			}
		})
	}
}

func TestNoMatchDoesNotDiscoverOllamaModels(t *testing.T) {
	runner := &questionRunner{repo: t.TempDir(), packet: `{"version":"1.0","status":"no_match","graphFingerprint":"fixture"}`}
	resolveCalls := 0
	llm := &deferredModel{client: &ollama.Client{}, resolve: func(string, bool) (string, error) { resolveCalls++; return "", fmt.Errorf("Ollama is offline") }}
	handleQuestion(runner, llm, "How does nonexistentConcept work?", runner.repo)
	if resolveCalls != 0 {
		t.Fatal("no-match required Ollama model discovery")
	}
}

func TestQuestionPassesFullIDAndVerifiedSourceToModel(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\nfunc Reconcile() {\n// AutoRepair branch evidence\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	id := "function:github.com/example/repo/pkg.CAPI.Reconcile"
	packet := fmt.Sprintf(`{"version":"1.0","status":"ok","graphFingerprint":"fixture","graph":{"repository":%q},"entities":[{"id":%q,"kind":"function"}],"sources":[{"entityID":%q,"role":"implementation","source":{"file":"a.go","line":2,"endLine":4},"reason":"AutoRepair selector"}]}`, repo, id, id)
	runner := &questionRunner{repo: repo, packet: packet}
	llm := &recordingLLM{}
	question := "How does autoRepair work for NodePool in " + id + "?"
	handleQuestion(runner, llm, question, repo)
	if llm.calls != 1 || !strings.Contains(llm.prompt, "AutoRepair branch evidence") || !strings.Contains(llm.prompt, id) {
		t.Fatalf("model input lost source/identity: calls=%d prompt=%s", llm.calls, llm.prompt)
	}
	for _, call := range runner.calls {
		if call[0] != "evidence" {
			continue
		}
		foundQuestion, foundID := false, false
		for i := 0; i < len(call)-1; i++ {
			if call[i] == "--question" && call[i+1] == question {
				foundQuestion = true
			}
			if call[i] == "--entity" && call[i+1] == id {
				foundID = true
			}
		}
		if !foundQuestion || !foundID {
			t.Fatalf("question/ID damaged in Atlas query: %v", call)
		}
		return
	}
	t.Fatal("no evidence query made")
}
