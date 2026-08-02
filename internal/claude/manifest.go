package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

type Manifest struct {
	Controller    string   `json:"controller"`
	ImplFiles     []string `json:"impl_files"`
	TestFiles     []string `json:"test_files"`
	TypeFiles     []string `json:"type_files"`
	Functions     []string `json:"functions"`
	FrameworkPath string   `json:"framework_path,omitempty"`
	Components    []string `json:"components,omitempty"`
}

func buildManifest(workload string, ws *workingset.WorkingSet, framework *prompt.FrameworkInfo, apiTypeFiles []string) *Manifest {
	m := &Manifest{
		Controller: workload,
		Functions:  ws.Functions,
		TypeFiles:  apiTypeFiles,
	}

	for _, f := range ws.ImplFiles {
		if !strings.Contains(f.Path, "→") {
			m.ImplFiles = append(m.ImplFiles, f.Path)
		}
	}
	for _, f := range ws.TestFiles {
		m.TestFiles = append(m.TestFiles, f.Path)
	}

	if framework != nil {
		m.FrameworkPath = framework.RelPath
		for _, line := range strings.Split(framework.Components, "\n") {
			parts := strings.SplitN(line, "|", 3)
			if len(parts) >= 2 {
				name := strings.TrimSpace(strings.TrimSuffix(parts[0], "/"))
				m.Components = append(m.Components, filepath.Join(framework.RelPath, name))
			}
		}
	}

	return m
}

func writeManifest(path string, m *Manifest) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling manifest: %v\n", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing manifest: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "--- Manifest saved: %s ---\n", path)
	}
}
