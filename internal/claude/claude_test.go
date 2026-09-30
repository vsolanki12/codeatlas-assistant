package claude

import (
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
	"testing"
)

func TestToPromptFilesPreservesTestEvidence(t *testing.T) {
	files := toPromptFiles([]workingset.FileContent{{Path: "a_test.go", Code: "func TestA() {}", Evidence: "tested_by (inferred), a_test.go:1316"}})
	if len(files) != 1 || files[0].Evidence != "tested_by (inferred), a_test.go:1316" {
		t.Fatalf("lost test evidence: %+v", files)
	}
}
