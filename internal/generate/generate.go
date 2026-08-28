package generate

import (
	"fmt"
	"os"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/intent"
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

	fmt.Fprintln(os.Stderr, "--- Extracting context ---")
	terms := intent.ExtractTechnicalTerms(description)

	if len(terms) == 0 {
		terms = strings.Fields(description)
		if len(terms) > 5 {
			terms = terms[:5]
		}
	}

	if len(terms) > 6 {
		terms = terms[:6]
	}
	if len(terms) == 0 {
		fmt.Fprintln(os.Stderr, "no technical terms found in request")
		return
	}

	fmt.Fprintf(os.Stderr, "terms: %s\n", strings.Join(terms, ", "))

	fmt.Fprintln(os.Stderr, "--- Gathering atlas context ---")
	var atlasData strings.Builder

	for _, term := range terms {
		result, err := atlasRun(a, "search", term, "--compact")
		if err != nil || strings.Contains(result, "No matching") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Search: %s\n%s\n", term, result))
	}

	topTerm := terms[0]
	if _, structured := a.(atlas.JSONRunner); structured {
		result, err := atlasRun(a, "ask", topTerm, "--intent", "debug", "--compact")
		if err == nil && !atlas.IsAmbiguous(result) && !strings.Contains(result, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Ask (debug): %s\n%s\n", topTerm, result))
		}
	} else {
		investigateResult, err := atlasRun(a, "investigate", topTerm)
		if err == nil && !strings.Contains(investigateResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Investigate: %s\n%s\n", topTerm, investigateResult))
		}

		explainResult, err := atlasRun(a, "explain", topTerm)
		if err == nil && !strings.Contains(explainResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Explain: %s\n%s\n", topTerm, explainResult))
		}
	}
	if strings.TrimSpace(atlasData.String()) == "" {
		fmt.Fprintln(os.Stderr, "atlas error: no CodeAtlas evidence matched this request; refusing to generate code")
		return
	}

	styleCode := style.LoadReference(styleFile, atlasData.String(), a.GraphPath())

	fmt.Fprintln(os.Stderr, "--- Generating code ---")
	var p string
	if repoPath != "" {
		ws := workingset.Build(repoPath, atlasData.String(), "", "", a)
		if len(ws.ImplFiles) == 0 {
			fmt.Fprintln(os.Stderr, "atlas error: no graph-selected implementation source was available; refusing to generate code")
			return
		}
		p = prompt.BuildGenerateWithSources(description, atlasData.String(), styleCode, conventions, toPromptFiles(ws.ImplFiles))
	} else {
		p = prompt.BuildGenerate(description, atlasData.String(), styleCode, conventions)
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
		out[i] = prompt.FileContent{Path: f.Path, Code: f.Code}
	}
	return out
}

func atlasRun(a atlas.Runner, args ...string) (string, error) {
	if jr, ok := a.(atlas.JSONRunner); ok {
		return jr.RunJSON(args...)
	}
	return a.Run(args...)
}
