package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/benchmark"
	"github.com/vsolanki12/codeatlas-assistant/internal/claude"
	"github.com/vsolanki12/codeatlas-assistant/internal/generate"
	"github.com/vsolanki12/codeatlas-assistant/internal/intent"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
	"github.com/vsolanki12/codeatlas-assistant/internal/solve"
	"github.com/vsolanki12/codeatlas-assistant/internal/validate"
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

func main() {
	model := flag.String("model", "", "ollama model name (auto-detect if empty)")
	graphPath := flag.String("graph", "atlas.json", "path to atlas graph JSON")
	atlasBinary := flag.String("atlas-bin", "", "CodeAtlas executable (default: CODEATLAS_BIN or atlas on PATH)")
	numCtx := flag.Int("num-ctx", 24576, "ollama context size")
	maxOutput := flag.Int("max-output", 1800, "maximum generated tokens")
	evidenceBudget := flag.Int("evidence-budget-bytes", 16384, "maximum compact Atlas evidence packet bytes (2048 to 1048576)")
	sourceBudget := flag.Int("source-budget-bytes", 24000, "maximum materialized source context bytes (1 to 1048576)")
	interactive := flag.Bool("interactive", false, "interactive REPL mode")
	solveFlag := flag.String("solve", "", "solve a JIRA issue (pass description text)")
	solveFile := flag.String("solve-file", "", "solve a JIRA issue (read description from file)")
	claudeFlag := flag.Bool("claude", false, "save Claude prompt to file + show analysis on screen")
	claudeFile := flag.String("claude-file", "", "read JIRA from file, save Claude prompt + show analysis")
	outputFile := flag.String("output", "", "output file for Claude prompt (auto-named if empty)")
	repoPath := flag.String("repo", "", "same source checkout as the graph (enables freshness checks and graph-selected source snippets)")
	generateFlag := flag.String("generate", "", "generate Go code (describe what to write)")
	reviewFile := flag.String("review-file", "", "review a local PR packet from a file")
	reviewDiff := flag.String("review-diff", "", "build a deterministic CodeAtlas PR packet from a Git diff file or -")
	reviewBase := flag.String("review-base", "", "base Git ref for --review-diff")
	reviewHead := flag.String("review-head", "HEAD", "head Git ref for --review-diff")
	styleFile := flag.String("style-file", "", "explicit Go file to use as style reference")
	conventionsFile := flag.String("conventions", "", "conventions file (embedded default if empty)")
	forceSolve := flag.Bool("force-solve", false, "skip existing fix check in solve mode")
	distillOnly := flag.Bool("distill-only", false, "generate XML + manifest only, skip solve step")
	benchmarkEntity := flag.String("benchmark", "", "measure full/compact Atlas context for an entity (no LLM)")
	benchmarkFixtures := flag.String("benchmark-fixtures", "", "JSON retrieval task fixtures (no LLM)")
	benchmarkBudgets := flag.String("benchmark-budgets", "2048,4096,8192,16384", "comma-separated evidence packet byte budgets for task fixtures")
	benchmarkJSON := flag.Bool("benchmark-json", false, "emit --benchmark result as JSON")
	flag.Parse()
	if err := validateContextBudgets(*evidenceBudget, *sourceBudget); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if *numCtx < 8192 {
		fmt.Fprintln(os.Stderr, "error: --num-ctx must be at least 8192")
		os.Exit(1)
	}
	if *maxOutput < 256 {
		fmt.Fprintln(os.Stderr, "error: --max-output must be at least 256")
		os.Exit(1)
	}

	a := &atlas.Client{Path: *graphPath, Binary: *atlasBinary}
	a.SetContextBudgets(*evidenceBudget, *sourceBudget)
	if *benchmarkFixtures != "" {
		data, err := os.ReadFile(*benchmarkFixtures)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchmark error: %v\n", err)
			os.Exit(1)
		}
		var tasks []benchmark.Task
		if err := json.Unmarshal(data, &tasks); err != nil {
			fmt.Fprintf(os.Stderr, "benchmark fixtures: %v\n", err)
			os.Exit(1)
		}
		var budgets []int
		for _, raw := range strings.Split(*benchmarkBudgets, ",") {
			budget, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				fmt.Fprintln(os.Stderr, "benchmark budgets must be integers")
				os.Exit(1)
			}
			budgets = append(budgets, budget)
		}
		results, err := benchmark.Evaluate(a, tasks, budgets, *repoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchmark error: %v\n", err)
			os.Exit(1)
		}
		failed := false
		if *benchmarkJSON {
			if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
				fmt.Fprintf(os.Stderr, "benchmark output: %v\n", err)
				os.Exit(1)
			}
		}
		for _, result := range results {
			if !result.Passed {
				failed = true
			}
			if !*benchmarkJSON {
				fmt.Printf("%s budget=%d passed=%t status=%s files=%d lines=%d prompt_estimated_tokens=%d failures=%v\n", result.TaskID, result.BudgetBytes, result.Passed, result.Status, result.UniqueFiles, result.UniqueSourceLines, result.Prompt.EstimatedTokens, result.Failures)
			}
		}
		if failed {
			os.Exit(1)
		}
		return
	}
	if *benchmarkEntity != "" {
		result, err := benchmark.Run(a, *benchmarkEntity, *repoPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchmark error: %v\n", err)
			os.Exit(1)
		}
		if *benchmarkJSON {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintf(os.Stderr, "benchmark output error: %v\n", err)
				os.Exit(1)
			}
		} else {
			fmt.Print(result.Format())
		}
		return
	}
	if *benchmarkJSON {
		fmt.Fprintln(os.Stderr, "error: --benchmark-json requires --benchmark <entity> or --benchmark-fixtures <path>")
		os.Exit(1)
	}

	heavy := *claudeFile != "" || *solveFile != "" || *solveFlag != "" || *generateFlag != "" || *reviewFile != "" || *reviewDiff != ""

	defer func() {
		if usage := a.LastUsage(); usage.Calls > 0 {
			fmt.Fprintf(os.Stderr, "Atlas usage: %s\n", usage.Summary())
		}
	}()
	llm := &deferredModel{client: &ollama.Client{NumCtx: *numCtx, MaxOutput: *maxOutput}, model: *model, heavy: heavy, resolve: resolveModel}
	conventions := prompt.LoadConventions(*conventionsFile)

	if *reviewFile != "" || *reviewDiff != "" {
		if *reviewFile != "" && *reviewDiff != "" {
			fmt.Fprintln(os.Stderr, "error: use only one of --review-file or --review-diff")
			os.Exit(1)
		}
		freshness := atlas.CheckFreshness(a, *repoPath)
		if freshness.BlocksImplementation() {
			fmt.Fprintf(os.Stderr, "atlas error: %s\n", freshness.Warning())
			return
		}
		reviewRepo := *repoPath
		if reviewRepo == "" {
			reviewRepo = freshness.GraphRepository
		}

		var reviewPacket string
		var err error
		if *reviewDiff != "" {
			reviewPacket, err = deterministicReviewPacket(a, *reviewDiff, *reviewBase, *reviewHead, reviewRepo)
		} else {
			var data []byte
			data, err = os.ReadFile(*reviewFile)
			if err == nil {
				reviewPacket = string(data)
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading review packet: %v\n", err)
			os.Exit(1)
		}
		if reviewPacket == "" {
			fmt.Fprintln(os.Stderr, "error: review packet is empty")
			os.Exit(1)
		}
		packetLimit := reviewPacketLimit(*numCtx, *maxOutput)
		reviewPacket = prompt.LimitReviewPacket(reviewPacket, packetLimit)
		reviewPrompt := prompt.BuildReview(reviewPacket, conventions)
		if reviewPrompt == "" {
			fmt.Fprintln(os.Stderr, "error: could not build review prompt")
			os.Exit(1)
		}
		output, err := llm.GenerateString(reviewPrompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
			os.Exit(1)
		}
		if err := publishGeneratedOutput("review", output, reviewRepo, a); err != nil {
			os.Exit(1)
		}
		return
	}

	if *claudeFile != "" {
		data, err := os.ReadFile(*claudeFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			os.Exit(1)
		}
		out := *outputFile
		if out == "" {
			out = claude.DefaultOutputName(*claudeFile)
		}
		claude.Run(a, llm, string(data), conventions, out, *repoPath, *distillOnly)
		return
	}

	if *solveFile != "" {
		data, err := os.ReadFile(*solveFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			os.Exit(1)
		}
		if *claudeFlag {
			out := *outputFile
			if out == "" {
				out = claude.DefaultOutputName(*solveFile)
			}
			claude.Run(a, llm, string(data), conventions, out, *repoPath, *distillOnly)
		} else {
			solve.Run(a, llm, string(data), conventions, *forceSolve, *repoPath)
		}
		return
	}

	if *solveFlag != "" {
		if *claudeFlag {
			out := *outputFile
			if out == "" {
				out = "claude-prompt.xml"
			}
			claude.Run(a, llm, *solveFlag, conventions, out, *repoPath, *distillOnly)
		} else {
			solve.Run(a, llm, *solveFlag, conventions, *forceSolve, *repoPath)
		}
		return
	}

	if *generateFlag != "" {
		generate.Run(a, llm, *generateFlag, *styleFile, conventions, *repoPath)
		return
	}

	if *interactive {
		runREPL(a, llm, conventions, *repoPath)
		return
	}

	question := strings.Join(flag.Args(), " ")
	if question == "" {
		fmt.Fprintln(os.Stderr, "usage: assistant [flags] \"your question\"")
		fmt.Fprintln(os.Stderr, "       assistant --solve \"JIRA description text\"")
		fmt.Fprintln(os.Stderr, "       assistant --solve-file jira.txt")
		fmt.Fprintln(os.Stderr, "       assistant --solve-file jira.txt --claude")
		fmt.Fprintln(os.Stderr, "       assistant --solve-file jira.txt --claude --output prompt.xml")
		fmt.Fprintln(os.Stderr, "       assistant --solve-file jira.txt --claude --repo ~/repo")
		fmt.Fprintln(os.Stderr, "       assistant --claude-file jira.txt")
		fmt.Fprintln(os.Stderr, "       assistant --generate \"add a validation function for NodePool\"")
		fmt.Fprintln(os.Stderr, "       assistant --review-file review-packet.md --graph graph.json")
		fmt.Fprintln(os.Stderr, "       assistant --review-diff diff.patch --review-base origin/main --repo ~/repo")
		fmt.Fprintln(os.Stderr, "       assistant --benchmark controller:example.com/repo/pkg.Reconciler --graph graph.json --repo ~/repo")
		fmt.Fprintln(os.Stderr, "       assistant --interactive")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags: --model name, --graph path, --atlas-bin path, --num-ctx N, --max-output N, --conventions file, --review-file packet, --benchmark entity, --benchmark-json")
		os.Exit(1)
	}

	if err := handleQuestion(a, llm, question, *repoPath); err != nil {
		os.Exit(1)
	}
}

func validateContextBudgets(evidence, source int) error {
	if evidence < 2048 || evidence > 1048576 {
		return fmt.Errorf("--evidence-budget-bytes must be between 2048 and 1048576")
	}
	if source < 1 || source > 1048576 {
		return fmt.Errorf("--source-budget-bytes must be between 1 and 1048576")
	}
	return nil
}

func deterministicReviewPacket(a atlas.Runner, diffPath, base, head, repo string) (string, error) {
	args := []string{"review", "--diff", diffPath, "--head", head}
	if base != "" {
		args = append(args, "--base", base)
	}
	if repo != "" {
		args = append(args, "--repo", repo)
	}
	if runner, ok := a.(atlas.JSONRunner); ok {
		return runner.RunJSON(args...)
	}
	return a.Run(append(args, "--json")...)
}

func reviewPacketLimit(numCtx, maxOutput int) int {
	usableTokens := numCtx - maxOutput - 2000
	if usableTokens < 2048 {
		usableTokens = 2048
	}
	limit := int(float64(usableTokens) * 3 * 0.70)
	if limit < 8000 {
		return 8000
	}
	return limit
}

var heavyModels = []string{"qwen2.5-coder:32b", "qwen3:30b", "qwen3:14b", "qwen3:8b"}
var lightModels = []string{"qwen2.5-coder:32b", "qwen3:30b", "qwen3:14b", "qwen3:8b"}

// Model discovery is deferred until retrieval and source verification succeed.
// A no-match result can therefore be reported even when Ollama is not running.
type deferredModel struct {
	client  *ollama.Client
	model   string
	heavy   bool
	resolve func(string, bool) (string, error)
}

func (m *deferredModel) prepare() error {
	if m.client == nil {
		return fmt.Errorf("model client is unavailable")
	}
	if m.client.Model != "" {
		return nil
	}
	resolve := m.resolve
	if resolve == nil {
		resolve = resolveModel
	}
	model, err := resolve(m.model, m.heavy)
	if err != nil {
		return err
	}
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("model discovery returned an empty model name")
	}
	m.client.Model = model
	return nil
}

func (m *deferredModel) Generate(p string) error {
	if err := m.prepare(); err != nil {
		return err
	}
	return m.client.Generate(p)
}

func (m *deferredModel) GenerateString(p string) (string, error) {
	if err := m.prepare(); err != nil {
		return "", err
	}
	return m.client.GenerateString(p)
}

func resolveModel(model string, heavy bool) (string, error) {
	if model != "" {
		return model, nil
	}

	models, err := ollama.ListModels()
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no ollama models installed — run: ollama pull qwen3:14b")
	}

	preferred := lightModels
	if heavy {
		preferred = heavyModels
	}

	available := make(map[string]bool, len(models))
	for _, m := range models {
		available[m] = true
	}
	for _, pref := range preferred {
		if available[pref] {
			kind := "light"
			if heavy {
				kind = "heavy"
			}
			fmt.Fprintf(os.Stderr, "using model: %s (%s task)\n", pref, kind)
			return pref, nil
		}
	}

	fmt.Fprintf(os.Stderr, "using model: %s\n", models[0])
	return models[0], nil
}

func handleQuestion(a atlas.Runner, llm ollama.LLM, question, repoPath string) error {
	freshness := atlas.CheckFreshness(a, repoPath)
	if !freshness.Available || freshness.Stale || freshness.RepositoryMismatch {
		fmt.Fprintf(os.Stderr, "atlas error: %s\n", freshness.Warning())
		return fmt.Errorf("source graph is unavailable or stale: %s", freshness.Warning())
	}
	if warning := freshness.Warning(); warning != "" {
		fmt.Fprintf(os.Stderr, "atlas warning: %s\n", warning)
	}

	i := intent.Detect(question)
	if i == intent.Stats {
		output, err := a.Run("stats")
		if err != nil {
			fmt.Fprintf(os.Stderr, "atlas error: %v\n", err)
			return err
		}
		fmt.Print(output)
		return nil
	}
	if freshness.BlocksImplementation() {
		fmt.Fprintf(os.Stderr, "atlas error: source evidence cannot be verified: %s\n", freshness.Warning())
		return fmt.Errorf("source evidence cannot be verified: %s", freshness.Warning())
	}
	queryIntent := "understand"
	if i == intent.Impact {
		queryIntent = "impact"
	}
	if i == intent.Investigate {
		queryIntent = "debug"
	}
	packet, _, err := atlas.QueryEvidence(a, atlas.EvidenceRequest{
		Question: question, Entity: intent.ExactEntityID(question), Intent: queryIntent,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas error: %v\n", err)
		return err
	}
	if repoPath == "" {
		repoPath = packet.Repository()
	}
	ws, err := workingset.FromEvidence(repoPath, packet, atlas.SourceBudget(a))
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas error: %v\n", err)
		return err
	}
	p := prompt.BuildAsk(question, packet.PromptEvidence()+"\n\n"+ws.PromptContext(), i.String())

	output, err := llm.GenerateString(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		return err
	}
	return publishGeneratedOutput("answer", output, repoPath, a)
}

// Validate repository references before publishing buffered model output. This
// checks known paths and IDs; it does not establish behavioral correctness.
func publishGeneratedOutput(kind, output, repoPath string, a atlas.Runner) error {
	if strings.TrimSpace(output) == "" {
		err := fmt.Errorf("%s withheld: model returned no content", kind)
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	result := validate.Output(output, repoPath, a)
	if !result.OK() {
		err := fmt.Errorf("%s withheld: generated repository references could not be verified", kind)
		fmt.Fprintf(os.Stderr, "%s\n%sUse the cited graph entity IDs or refine the question before retrying.\n", err, result.Report())
		return err
	}
	fmt.Print(output)
	return nil
}

func modelLabel(llm ollama.LLM) string {
	switch model := llm.(type) {
	case *deferredModel:
		if model != nil {
			if model.client != nil && model.client.Model != "" {
				return model.client.Model
			}
			if model.model != "" {
				return model.model
			}
		}
		return "auto (selected after retrieval)"
	case *ollama.Client:
		if model != nil && model.Model != "" {
			return model.Model
		}
		return "auto"
	default:
		return "custom"
	}
}

func runREPL(a atlas.Runner, llm ollama.LLM, conventions, repoPath string) {
	fmt.Println("CodeAtlas Assistant (type 'exit' to quit)")
	fmt.Println("  prefix with 'solve:' to analyze a JIRA description")
	fmt.Println("  prefix with 'claude:' to generate Claude prompt")
	fmt.Println("  prefix with 'gen:' to generate Go code")
	fmt.Printf("  graph: %s | model: %s\n", a.GraphPath(), modelLabel(llm))
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			break
		}

		if strings.HasPrefix(input, "solve:") {
			jiraText := strings.TrimSpace(strings.TrimPrefix(input, "solve:"))
			if jiraText != "" {
				solve.Run(a, llm, jiraText, conventions, false, repoPath)
			}
		} else if strings.HasPrefix(input, "claude:") {
			jiraText := strings.TrimSpace(strings.TrimPrefix(input, "claude:"))
			if jiraText != "" {
				claude.Run(a, llm, jiraText, conventions, "claude-prompt.xml", repoPath, false)
			}
		} else if strings.HasPrefix(input, "gen:") {
			desc := strings.TrimSpace(strings.TrimPrefix(input, "gen:"))
			if desc != "" {
				generate.Run(a, llm, desc, "", conventions, repoPath)
			}
		} else {
			handleQuestion(a, llm, input, repoPath)
		}
		fmt.Println()
	}
}
