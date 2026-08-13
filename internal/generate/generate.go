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
)

func Run(a atlas.Runner, llm ollama.LLM, description, styleFile, conventions string) {
	if f := atlas.CheckFreshness(a, ""); f.Stale {
		fmt.Fprintf(os.Stderr, "--- WARNING: %s ---\n", f.Warning())
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

	fmt.Fprintf(os.Stderr, "terms: %s\n", strings.Join(terms, ", "))

	fmt.Fprintln(os.Stderr, "--- Gathering atlas context ---")
	var atlasData strings.Builder

	for _, term := range terms {
		result, err := a.Run("search", term)
		if err != nil || strings.Contains(result, "No matching") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Search: %s\n%s\n", term, result))
	}

	topTerm := terms[0]
	investigateResult, err := a.Run("investigate", topTerm)
	if err == nil && !strings.Contains(investigateResult, "not found") {
		atlasData.WriteString(fmt.Sprintf("### Investigate: %s\n%s\n", topTerm, investigateResult))
	}

	explainResult, err := a.Run("explain", topTerm)
	if err == nil && !strings.Contains(explainResult, "not found") {
		atlasData.WriteString(fmt.Sprintf("### Explain: %s\n%s\n", topTerm, explainResult))
	}

	styleCode := style.LoadReference(styleFile, atlasData.String(), a.GraphPath())

	fmt.Fprintln(os.Stderr, "--- Generating code ---")
	p := prompt.BuildGenerate(description, atlasData.String(), styleCode, conventions)

	output, err := llm.GenerateString(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		return
	}
	fmt.Print(output)

	vr := validate.Output(output, "", a)
	if !vr.OK() {
		fmt.Fprintf(os.Stderr, "\n--- VALIDATION WARNING ---\n%s", vr.Report())
	}
}
