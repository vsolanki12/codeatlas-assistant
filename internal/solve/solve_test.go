package solve

import (
	"testing"

	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

func TestToPromptFilesPreservesGraphEvidence(t *testing.T) {
	files := toPromptFiles([]workingset.FileContent{{
		Path:     "pkg/a_test.go",
		Code:     "func TestA() {}",
		Evidence: "tested_by (inferred), evidence pkg/a_test.go:12",
	}})
	if len(files) != 1 {
		t.Fatalf("got %d prompt files, want 1", len(files))
	}
	if files[0].Evidence != "tested_by (inferred), evidence pkg/a_test.go:12" {
		t.Fatalf("prompt evidence = %q, want working-set graph evidence", files[0].Evidence)
	}
}
