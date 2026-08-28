package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/claude"
	"github.com/vsolanki12/codeatlas-assistant/internal/generate"
	"github.com/vsolanki12/codeatlas-assistant/internal/intent"
	"github.com/vsolanki12/codeatlas-assistant/internal/ollama"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
	"github.com/vsolanki12/codeatlas-assistant/internal/solve"
	"github.com/vsolanki12/codeatlas-assistant/internal/validate"
)

func main() {
	model := flag.String("model", "", "ollama model name (auto-detect if empty)")
	graphPath := flag.String("graph", "atlas.json", "path to atlas graph JSON")
	numCtx := flag.Int("num-ctx", 24576, "ollama context size")
	maxOutput := flag.Int("max-output", 1800, "maximum generated tokens")
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
	styleFile := flag.String("style-file", "", "Go file to use as style reference (auto-detect if empty)")
	conventionsFile := flag.String("conventions", "", "conventions file (embedded default if empty)")
	forceSolve := flag.Bool("force-solve", false, "skip existing fix check in solve mode")
	distillOnly := flag.Bool("distill-only", false, "generate XML + manifest only, skip solve step")
	flag.Parse()
	if *numCtx < 8192 {
		fmt.Fprintln(os.Stderr, "error: --num-ctx must be at least 8192")
		os.Exit(1)
	}
	if *maxOutput < 256 {
		fmt.Fprintln(os.Stderr, "error: --max-output must be at least 256")
		os.Exit(1)
	}

	heavy := *claudeFile != "" || *solveFile != "" || *solveFlag != "" || *generateFlag != "" || *reviewFile != "" || *reviewDiff != ""
	resolvedModel, err := resolveModel(*model, heavy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	a := &atlas.Client{Path: *graphPath}
	llm := &ollama.Client{Model: resolvedModel, NumCtx: *numCtx, MaxOutput: *maxOutput}
	conventions := prompt.LoadConventions(*conventionsFile)

	if *reviewFile != "" || *reviewDiff != "" {
		if *reviewFile != "" && *reviewDiff != "" {
			fmt.Fprintln(os.Stderr, "error: use only one of --review-file or --review-diff")
			os.Exit(1)
		}
		freshness := atlas.CheckFreshness(a, *repoPath)
		if !freshness.Available || freshness.Stale || freshness.RepositoryMismatch || freshness.Dirty || freshness.Incomplete || freshness.GraphCommit == "" || freshness.EntityIdentity == "" || !freshness.Verifiable || !freshness.StateVerifiable {
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
		fmt.Print(output)
		if result := validate.Output(output, *repoPath, a); !result.OK() {
			fmt.Fprintf(os.Stderr, "\n--- VALIDATION WARNING ---\n%s", result.Report())
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
		fmt.Fprintln(os.Stderr, "       assistant --solve-file jira.txt --claude --repo ~/hypershift")
		fmt.Fprintln(os.Stderr, "       assistant --claude-file jira.txt")
		fmt.Fprintln(os.Stderr, "       assistant --generate \"add a validation function for NodePool\"")
		fmt.Fprintln(os.Stderr, "       assistant --review-file review-packet.md --graph graph.json")
		fmt.Fprintln(os.Stderr, "       assistant --review-diff diff.patch --review-base origin/main --repo ~/repo")
		fmt.Fprintln(os.Stderr, "       assistant --interactive")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags: --model name, --graph path, --num-ctx N, --max-output N, --conventions file, --review-file packet")
		os.Exit(1)
	}

	handleQuestion(a, llm, question, *repoPath)
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

func handleQuestion(a atlas.Runner, llm ollama.LLM, question, repoPath string) {
	freshness := atlas.CheckFreshness(a, repoPath)
	if !freshness.Available || freshness.Stale || freshness.RepositoryMismatch {
		fmt.Fprintf(os.Stderr, "atlas error: %s\n", freshness.Warning())
		return
	}
	if warning := freshness.Warning(); warning != "" {
		fmt.Fprintf(os.Stderr, "atlas warning: %s\n", warning)
	}

	i := intent.Detect(question)
	entity := intent.ExtractEntity(question)

	fmt.Fprintf(os.Stderr, "intent: %s | entity: %q\n", i, entity)

	atlasOutput, err := runForIntent(a, entity, i)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas error: %v\n", err)
		os.Exit(1)
	}
	if atlas.IsAmbiguous(atlasOutput) {
		fmt.Fprintln(os.Stderr, "atlas error: entity name is ambiguous; rerun with the exact CodeAtlas entity ID")
		for _, ref := range atlas.EntityRefs(atlasOutput) {
			fmt.Fprintf(os.Stderr, "  candidate: %s\n", ref.ID)
		}
		return
	}
	if freshness.Warning() != "" {
		atlasOutput = freshness.PromptContext() + "\n\n## CodeAtlas Query Evidence\n" + atlasOutput
	}

	p := prompt.BuildAsk(question, atlasOutput, i.String())

	output, err := llm.GenerateString(p)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(output)
	if result := validate.Output(output, repoPath, a); !result.OK() {
		fmt.Fprintf(os.Stderr, "\n--- VALIDATION WARNING ---\n%s", result.Report())
	}
}

func runForIntent(a atlas.Runner, entity string, i intent.Intent) (string, error) {
	if jsonRunner, ok := a.(atlas.JSONRunner); ok {
		switch i {
		case intent.Explain:
			return jsonRunner.RunJSON("ask", entity, "--intent", "understand", "--compact")
		case intent.Impact:
			return jsonRunner.RunJSON("ask", entity, "--intent", "impact", "--compact")
		case intent.Investigate:
			return jsonRunner.RunJSON("ask", entity, "--intent", "debug", "--compact")
		case intent.Search:
			return jsonRunner.RunJSON("search", entity, "--compact")
		case intent.Stats:
			return jsonRunner.RunJSON("stats")
		default:
			return jsonRunner.RunJSON("ask", entity, "--compact")
		}
	}

	switch i {
	case intent.Explain:
		return multiSource(a, entity, "explain", "investigate")
	case intent.Impact:
		return multiSource(a, entity, "impact", "context")
	case intent.Investigate:
		return multiSource(a, entity, "investigate", "explain")
	case intent.Search:
		return a.Run("search", entity)
	case intent.Stats:
		return a.Run("stats")
	default:
		return a.Run("ask", entity)
	}
}

func multiSource(a atlas.Runner, entity string, commands ...string) (string, error) {
	var combined strings.Builder
	for _, cmd := range commands {
		result, err := a.Run(cmd, entity)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atlas %s: %v (skipping)\n", cmd, err)
			continue
		}
		if strings.Contains(result, "not found") || strings.Contains(result, "Empty") {
			continue
		}
		fmt.Fprintf(os.Stderr, "atlas %s: %d chars\n", cmd, len(result))
		combined.WriteString(fmt.Sprintf("### %s\n%s\n\n", cmd, result))
	}
	if combined.Len() == 0 {
		return "", fmt.Errorf("no atlas data found for %q", entity)
	}
	return combined.String(), nil
}

func runREPL(a atlas.Runner, llm ollama.LLM, conventions, repoPath string) {
	fmt.Println("CodeAtlas Assistant (type 'exit' to quit)")
	fmt.Println("  prefix with 'solve:' to analyze a JIRA description")
	fmt.Println("  prefix with 'claude:' to generate Claude prompt")
	fmt.Println("  prefix with 'gen:' to generate Go code")
	fmt.Printf("  graph: %s | model: %s\n", a.GraphPath(), llm.(*ollama.Client).Model)
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
