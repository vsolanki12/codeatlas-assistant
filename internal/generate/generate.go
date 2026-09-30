package generate

import (
	"fmt"
	"os"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/gather"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
	"github.com/vsolanki12/codeatlas-assistant/internal/style"
	"github.com/vsolanki12/codeatlas-assistant/internal/validate"
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

func Run(a atlas.Runner, llm ollama.LLM, description, styleFile, conventions, repoPath string) {
	if f := atlas.CheckFreshness(a, repoPath); f.BlocksImplementation() {
		fmt.Fprintf(os.Stderr, "--- ERROR: %s ---\n", f.Warning())
		return
	} else if warning := f.Warning(); warning != "" {
		fmt.Fprintf(os.Stderr, "--- WARNING: %s ---\n", warning)
	}

	result := gather.FromJIRA(a, description)
	if result.Error != nil || result.Packet == nil {
		return
	}
	ws, err := workingset.FromEvidence(repoPath, result.Packet, atlas.SourceBudget(a))
	if err != nil || len(ws.ImplFiles) == 0 {
		fmt.Fprintf(os.Stderr, "atlas error: implementation evidence is unavailable: %v\n", err)
		return
	}
	styleCode := ""
	if styleFile != "" {
		styleCode = style.LoadReference(styleFile, result.AtlasData, a.GraphPath())
	}
	files := append(toPromptFiles(ws.ImplFiles), toPromptFiles(ws.TestFiles)...)
	p := prompt.BuildGenerateWithSources(description, result.Packet.PromptEvidence(), styleCode, conventions, files)
	p += "\n\n## API definitions\n" + ws.Types
	for _, omission := range ws.Omissions {
		p += "\nSource limitation: " + omission
	}

	output, err := llm.GenerateString(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		return
	}

	vr := validate.Output(output, repoPath, a)
	if !vr.OK() {
		fmt.Fprintf(os.Stderr, "--- VALIDATION ERROR ---\n%s", vr.Report())
		return
	}
	fmt.Print(output)
	if vr.Checked > 0 {
		fmt.Fprintf(os.Stderr, "\n--- %s ---\n", vr.Report())
	}
}

func toPromptFiles(files []workingset.FileContent) []prompt.FileContent {
	out := make([]prompt.FileContent, len(files))
	for i, f := range files {
		out[i] = prompt.FileContent{Path: f.Path, Code: f.Code, Evidence: f.Evidence}
	}
	return out
}
