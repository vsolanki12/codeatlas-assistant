package workingset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
)

func evidenceFixture(t *testing.T, repo string) *atlas.EvidencePacket {
	t.Helper()
	graph, _ := json.Marshal(map[string]string{"repository": repo})
	return &atlas.EvidencePacket{Version: "1.0", Status: "ok", GraphFingerprint: "fixture", Graph: graph, Entities: []atlas.EvidenceEntity{{ID: "function:a", Kind: "function"}, {ID: "test:a", Kind: "test"}, {ID: "field:a", Kind: "field"}}, Sources: []atlas.EvidenceSource{
		{EntityID: "function:a", Role: "implementation", Source: atlas.SourceSpan{File: "a.go", Line: 2, EndLine: 102}, Reason: "selector AutoRepair"},
		{EntityID: "function:a", Role: "usage", Source: atlas.SourceSpan{File: "a.go", Line: 90, EndLine: 100}, Reason: "AutoRepair branch"},
		{EntityID: "test:a", Role: "test", Source: atlas.SourceSpan{File: "a_test.go", Line: 1316, EndLine: 1320}, Reason: "tested_by"},
		{EntityID: "field:a", Role: "definition", Source: atlas.SourceSpan{File: "types.go", Line: 2, EndLine: 3}, Reason: "API definition"},
	}}
}

func TestEvidenceMaterializesLargeFunctionAndExactTestSpan(t *testing.T) {
	repo := t.TempDir()
	implementation := "package a\nfunc Reconcile() {\n" + strings.Repeat("// implementation evidence larger than old 3000-byte skip\n", 99) + "}\n"
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte(implementation), 0644); err != nil {
		t.Fatal(err)
	}
	testSource := strings.Repeat("// unrelated file prefix\n", 1315) + "func TestReconcile() {\n// AutoRepair observed\n// assertion\n// suffix\n}\n"
	if err := os.WriteFile(filepath.Join(repo, "a_test.go"), []byte(testSource), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "types.go"), []byte("package a\n// AutoRepair enables health checks\nAutoRepair bool\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ws, err := FromEvidence(repo, evidenceFixture(t, repo), 24000)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.ImplFiles) != 1 || len(ws.ImplFiles[0].Code) < 3000 || !strings.Contains(ws.ImplFiles[0].Code, "func Reconcile") {
		t.Fatalf("large body missing or overlap not merged: %+v", ws.ImplFiles)
	}
	if len(ws.TestFiles) != 1 || !strings.Contains(ws.TestFiles[0].Code, "TestReconcile") || strings.Contains(ws.TestFiles[0].Code, "unrelated file prefix") {
		t.Fatalf("test span incorrect: %+v", ws.TestFiles)
	}
	if !strings.Contains(ws.TestFiles[0].Evidence, "a_test.go:1316-1320") || !strings.Contains(ws.Types, "AutoRepair bool") {
		t.Fatalf("API/test evidence lost: %s", ws.PromptContext())
	}
	if len(ws.Sources) != 3 {
		t.Fatalf("sources=%d, want merged implementation, test and definition", len(ws.Sources))
	}
}

func TestEvidenceBudgetReservesTestContextAndReportsOmissions(t *testing.T) {
	repo := t.TempDir()
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\nfunc Reconcile() {\n"+strings.Repeat("// long implementation\n", 99)+"}\n"), 0644)
	_ = os.WriteFile(filepath.Join(repo, "a_test.go"), []byte(strings.Repeat("// prefix\n", 1315)+"func TestReconcile() {\n// branch\n// assertion\n// suffix\n}\n"), 0644)
	_ = os.WriteFile(filepath.Join(repo, "types.go"), []byte("package a\n// AutoRepair\nAutoRepair bool\n"), 0644)
	ws, err := FromEvidence(repo, evidenceFixture(t, repo), 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.TestFiles) != 1 || !strings.Contains(ws.TestFiles[0].Code, "TestReconcile") {
		t.Fatal("implementation exhausted test budget")
	}
	if len(ws.Omissions) == 0 || ws.TotalChars() > 2048 {
		t.Fatalf("omissions=%v bytes=%d", ws.Omissions, ws.TotalChars())
	}
}

func TestEvidenceRejectsEscapingSymlinkAndUnavailableSpans(t *testing.T) {
	repo := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	_ = os.WriteFile(outside, []byte("package secret\n"), 0644)
	if err := os.Symlink(outside, filepath.Join(repo, "a.go")); err != nil {
		t.Fatal(err)
	}
	packet := evidenceFixture(t, repo)
	packet.Sources = packet.Sources[:1]
	if _, err := FromEvidence(repo, packet, 24000); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escaping source accepted: %v", err)
	}
	packet.Sources[0].Source.File = "missing.go"
	if _, err := FromEvidence(repo, packet, 24000); err == nil {
		t.Fatal("zero usable source accepted")
	}
}

func TestEvidenceSmallBudgetPreservesLateFocusAndHelperDeclaration(t *testing.T) {
	repo := t.TempDir()
	lines := make([]string, 1002)
	for i := range lines {
		lines[i] = fmt.Sprintf("// unrelated implementation line %d with substantial context padding", i+1)
	}
	lines[0] = "package a"
	lines[1] = "func Reconcile() {"
	lines[904] = "if nodePool.Management.AutoRepair { reconcileMachineHealthCheck() }"
	lines[1001] = "}"
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	helper := []string{"package a", "", "", "", "func reconcileMachineHealthCheck() {", "// required health-check implementation", "}", "", "", ""}
	if err := os.WriteFile(filepath.Join(repo, "helper.go"), []byte(strings.Join(helper, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "a_test.go"), []byte(strings.Repeat("// prefix\n", 1315)+"func TestReconcile() {\n// AutoRepair assertion\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "types.go"), []byte("package a\nAutoRepair bool\n"), 0644); err != nil {
		t.Fatal(err)
	}
	packet := evidenceFixture(t, repo)
	packet.Entities = append(packet.Entities, atlas.EvidenceEntity{ID: "function:helper", Kind: "function"})
	packet.Sources = []atlas.EvidenceSource{
		{EntityID: "function:a", Role: "implementation", Source: atlas.SourceSpan{Parser: "go-ast", File: "a.go", Line: 2, EndLine: 1002}, Reason: "large declaration"},
		{EntityID: "function:a", Role: "usage", Source: atlas.SourceSpan{Parser: "go-types", File: "a.go", Line: 897, EndLine: 915}, Reason: "AutoRepair conditional"},
		{EntityID: "function:helper", Role: "implementation", Source: atlas.SourceSpan{Parser: "go-ast", File: "helper.go", Line: 5, EndLine: 7}, Reason: "linked helper"},
		{EntityID: "test:a", Role: "test", Source: atlas.SourceSpan{Parser: "test", File: "a_test.go", Line: 1316, EndLine: 1318}, Reason: "linked test"},
		{EntityID: "field:a", Role: "definition", Source: atlas.SourceSpan{Parser: "go-ast", File: "types.go", Line: 2, EndLine: 2}, Reason: "API declaration"},
	}
	edge := atlas.RelationshipRef{From: "function:a", To: "field:a", Type: "references"}
	edge.Evidence.File, edge.Evidence.Line = "a.go", 905
	packet.Relationships = []atlas.RelationshipRef{edge}
	ws, err := FromEvidence(repo, packet, 2400)
	if err != nil {
		t.Fatal(err)
	}
	context := ws.PromptContext()
	if !strings.Contains(context, lines[904]) || !strings.Contains(context, "func reconcileMachineHealthCheck()") || !strings.Contains(context, "func TestReconcile()") || !strings.Contains(context, "AutoRepair bool") {
		t.Fatalf("required focused/helper/test/API evidence lost under budget:\n%s", context)
	}
	foundFocus := false
	for _, source := range ws.Sources {
		if source.EntityID == "function:a" && source.Source.Line <= 905 && source.Source.EndLine >= 905 {
			foundFocus = true
		}
		if source.EntityID == "function:helper" && (source.Source.File != "helper.go" || source.Source.Line < 5 || source.Source.EndLine > 7) {
			t.Fatalf("helper inherited another span: %+v", source)
		}
	}
	if !foundFocus || len(ws.Omissions) == 0 || ws.TotalChars() > 2400 {
		t.Fatalf("focus lineage/budget incorrect: %+v", ws)
	}
}

func TestEvidenceMergedLineageKeepsEachEntityCoordinates(t *testing.T) {
	repo := t.TempDir()
	var lines []string
	for i := 1; i <= 110; i++ {
		lines = append(lines, fmt.Sprintf("// source line %d", i))
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	packet := evidenceFixture(t, repo)
	packet.Entities = []atlas.EvidenceEntity{{ID: "function:outer", Kind: "function"}, {ID: "function:inner", Kind: "function"}}
	packet.Sources = []atlas.EvidenceSource{
		{EntityID: "function:outer", Role: "implementation", Source: atlas.SourceSpan{Parser: "go-ast", File: "a.go", Line: 2, EndLine: 102}, Reason: "enclosing declaration"},
		{EntityID: "function:inner", Role: "implementation", Source: atlas.SourceSpan{Parser: "go-ast", File: "a.go", Line: 90, EndLine: 100}, Reason: "nested selected span"},
	}
	ws, err := FromEvidence(repo, packet, 24000)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.ImplFiles) != 1 {
		t.Fatalf("overlapping selected source duplicated: %+v", ws.ImplFiles)
	}
	for _, source := range ws.Sources {
		if source.EntityID == "function:inner" && (source.Source.Line != 90 || source.Source.EndLine != 100 || source.Source.Parser != "go-ast") {
			t.Fatalf("merged entity inherited outer coordinates: %+v", source)
		}
	}
	for i := range lines {
		lines[i] += strings.Repeat(" padding", 12)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	ws, err = FromEvidence(repo, packet, 2048)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range ws.Sources {
		if source.EntityID == "function:inner" {
			found = true
			if source.Source.Line < 90 || source.Source.EndLine > 100 {
				t.Fatalf("partial merge fabricated inner lineage: %+v", source)
			}
		}
	}
	if !found || !strings.Contains(ws.PromptContext(), "source line 90") {
		t.Fatal("budget removed the nested span's declaration")
	}
}

func TestEvidenceCannotClaimUnmaterializedLongFocusLine(t *testing.T) {
	repo := t.TempDir()
	lines := []string{"package a", "func outer() {", strings.Repeat("very long indivisible source line ", 300), "}"}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	packet := evidenceFixture(t, repo)
	packet.Entities = []atlas.EvidenceEntity{{ID: "function:outer", Kind: "function"}, {ID: "function:focus", Kind: "function"}}
	packet.Sources = []atlas.EvidenceSource{
		{EntityID: "function:outer", Role: "implementation", Source: atlas.SourceSpan{File: "a.go", Line: 2, EndLine: 4}, Reason: "declaration"},
		{EntityID: "function:focus", Role: "usage", Source: atlas.SourceSpan{File: "a.go", Line: 3, EndLine: 3}, Reason: "indivisible focused source"},
	}
	ws, err := FromEvidence(repo, packet, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range ws.Sources {
		if source.EntityID == "function:focus" || (source.Source.Line <= 3 && source.Source.EndLine >= 3) {
			t.Fatalf("omitted line claimed as evidence: %+v", source)
		}
	}
	if containsString(ws.Functions, "function:focus") || len(ws.Omissions) == 0 {
		t.Fatal("omitted function incorrectly advertised")
	}
	if strings.Contains(ws.PromptContext(), "Atlas source a.go:2-4") {
		t.Fatal("header advertises source lines that were not materialized")
	}
}

func TestEvidenceSingleLineWithoutEndLineRetainsLineage(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\nfunc single() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	packet := evidenceFixture(t, repo)
	packet.Entities = packet.Entities[:1]
	packet.Sources = []atlas.EvidenceSource{{EntityID: "function:a", Role: "implementation", Source: atlas.SourceSpan{Parser: "go-ast", File: "a.go", Line: 2}, Reason: "single-line declaration"}}
	ws, err := FromEvidence(repo, packet, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.Sources) != 1 || ws.Sources[0].Source.Line != 2 || ws.Sources[0].Source.EndLine != 2 || !containsString(ws.Functions, "function:a") {
		t.Fatalf("implicit single-line span lost lineage: %+v", ws)
	}
}
