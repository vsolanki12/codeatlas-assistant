package atlas

import (
	"fmt"
	"strings"
	"testing"
)

type evidenceRunner struct {
	data string
	args []string
}

func (r *evidenceRunner) GraphPath() string                      { return "graph.json" }
func (r *evidenceRunner) Run(args ...string) (string, error)     { r.args = args; return r.data, nil }
func (r *evidenceRunner) RunJSON(args ...string) (string, error) { r.args = args; return r.data, nil }

func TestEvidenceFullQuestionAndExactIDRoundTrip(t *testing.T) {
	id := "function:github.com/example/repo/pkg.CAPI.Reconcile"
	runner := &evidenceRunner{data: fmt.Sprintf(`{"version":"1.0","status":"ok","graphFingerprint":"fixture","entities":[{"id":%q,"kind":"function"}],"sources":[{"entityID":%q,"role":"implementation","source":{"file":"pkg/a.go","line":10,"endLine":30}}]}`, id, id)}
	question := "How does autoRepair work for NodePool in " + id + "?"
	if _, _, err := QueryEvidence(runner, EvidenceRequest{Question: question, Entity: id, Intent: "understand", Scope: "NodePool"}); err != nil {
		t.Fatal(err)
	}
	wanted := map[string]string{"--question": question, "--entity": id, "--scope": "NodePool", "--budget-bytes": "16384"}
	for i := 0; i < len(runner.args)-1; i++ {
		if expected, ok := wanted[runner.args[i]]; ok {
			if runner.args[i+1] != expected {
				t.Fatalf("%s=%q, want %q", runner.args[i], runner.args[i+1], expected)
			}
			delete(wanted, runner.args[i])
		}
	}
	if len(wanted) > 0 {
		t.Fatalf("missing request fields %v", wanted)
	}
}

func TestEvidenceRetrievalFailuresAndCompatibility(t *testing.T) {
	for _, status := range []string{"no_match", "ambiguous", "budget_exhausted"} {
		t.Run(status, func(t *testing.T) {
			r := &evidenceRunner{data: fmt.Sprintf(`{"version":"1.0","status":%q,"graphFingerprint":"fixture"}`, status)}
			packet, _, err := QueryEvidence(r, EvidenceRequest{Question: "unknown", Intent: "understand"})
			if err == nil || packet == nil || packet.Status != status {
				t.Fatalf("packet=%+v error=%v", packet, err)
			}
		})
	}
	for _, data := range []string{`{"version":"2.0","status":"ok","graphFingerprint":"fixture"}`, `{"version":"1.0","status":"invented","graphFingerprint":"fixture"}`, `{"version":"1.0","status":"ok"}`, `{"version":"1.0","status":"ok","graphFingerprint":"fixture","sources":[{"entityID":"function:a","source":{"file":"a.go","line":1}}]}`} {
		if _, err := ParseEvidence(data); err == nil {
			t.Fatalf("accepted incompatible evidence %s", data)
		}
	}
}

func TestEvidenceNoSourcesIsNotGrounding(t *testing.T) {
	r := &evidenceRunner{data: `{"version":"1.0","status":"ok","graphFingerprint":"fixture","entities":[{"id":"function:a","kind":"function"}],"sources":[]}`}
	if _, _, err := QueryEvidence(r, EvidenceRequest{Question: "a"}); err == nil || !strings.Contains(err.Error(), "source-backed") {
		t.Fatalf("error=%v", err)
	}
}

func TestPromptEvidenceKeepsExactIDsAndProofOnce(t *testing.T) {
	packet := &EvidencePacket{Version: EvidenceVersion, Status: "ok", GraphFingerprint: "snapshot", Graph: []byte(`{"schemaVersion":"1.6.0"}`),
		Entities: []EvidenceEntity{{ID: "function:example.com/repo.CAPI.Reconcile", Name: "Reconcile", Kind: "function", Source: SourceSpan{File: "capi.go", Line: 10, EndLine: 20}}, {ID: "field:example.com/repo.Management.AutoRepair", Name: "AutoRepair", Kind: "field", Source: SourceSpan{File: "api.go", Line: 5}}}, Truncated: true}
	edge := RelationshipRef{From: packet.Entities[0].ID, To: packet.Entities[1].ID, Type: "references", Confidence: "proven"}
	edge.Evidence.File, edge.Evidence.Line, edge.Evidence.Parser = "capi.go", 15, "go-types"
	edge.Evidence.Snippet, edge.Evidence.Reason = "if pool.AutoRepair {", "typed selector proof"
	packet.Relationships = []RelationshipRef{edge}
	data := packet.PromptEvidence()
	for _, entity := range packet.Entities {
		if strings.Count(data, entity.ID) != 1 {
			t.Fatalf("exact ID missing or duplicated: %s", data)
		}
	}
	for _, expected := range []string{"E1 --references--> E2 (proven)", "go-types capi.go:15", "typed selector proof", "truncated: true", "snapshot", "1.6.0"} {
		if !strings.Contains(data, expected) {
			t.Fatalf("missing %q: %s", expected, data)
		}
	}
}

func TestClientContextBudgetDefaultsAndOverrides(t *testing.T) {
	client := &Client{}
	if EvidenceBudget(client) != DefaultEvidenceBudget || SourceBudget(client) != DefaultSourceBudget {
		t.Fatal("context budget defaults changed")
	}
	client.SetContextBudgets(8192, 12000)
	if EvidenceBudget(client) != 8192 || SourceBudget(client) != 12000 {
		t.Fatal("client context budgets were ignored")
	}
}
