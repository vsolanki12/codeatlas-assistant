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
)

func main() {
	model := flag.String("model", "", "ollama model name (auto-detect if empty)")
	graphPath := flag.String("graph", "atlas.json", "path to atlas graph JSON")
	interactive := flag.Bool("interactive", false, "interactive REPL mode")
	solveFlag := flag.String("solve", "", "solve a JIRA issue (pass description text)")
	solveFile := flag.String("solve-file", "", "solve a JIRA issue (read description from file)")
	claudeFlag := flag.Bool("claude", false, "save Claude prompt to file + show analysis on screen")
	claudeFile := flag.String("claude-file", "", "read JIRA from file, save Claude prompt + show analysis")
	outputFile := flag.String("output", "", "output file for Claude prompt (auto-named if empty)")
	repoPath := flag.String("repo", "", "path to source repo (injects file tree into Claude prompt)")
	generateFlag := flag.String("generate", "", "generate Go code (describe what to write)")
	styleFile := flag.String("style-file", "", "Go file to use as style reference (auto-detect if empty)")
	conventionsFile := flag.String("conventions", "", "conventions file (embedded default if empty)")
	forceSolve := flag.Bool("force-solve", false, "skip existing fix check in solve mode")
	distillOnly := flag.Bool("distill-only", false, "generate XML + manifest only, skip solve step")
	flag.Parse()

	heavy := *claudeFile != "" || *solveFile != "" || *solveFlag != "" || *generateFlag != ""
	resolvedModel, err := resolveModel(*model, heavy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	a := &atlas.Client{Path: *graphPath}
	llm := &ollama.Client{Model: resolvedModel}
	conventions := prompt.LoadConventions(*conventionsFile)

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
		generate.Run(a, llm, *generateFlag, *styleFile, conventions)
		return
	}

	if *interactive {
		runREPL(a, llm, conventions)
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
		fmt.Fprintln(os.Stderr, "       assistant --interactive")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags: --model name, --graph path, --conventions file")
		os.Exit(1)
	}

	handleQuestion(a, llm, question)
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

func handleQuestion(a atlas.Runner, llm ollama.LLM, question string) {
	i := intent.Detect(question)
	entity := intent.ExtractEntity(question)

	fmt.Fprintf(os.Stderr, "intent: %s | entity: %q\n", i, entity)

	atlasOutput, err := runForIntent(a, entity, i)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas error: %v\n", err)
		os.Exit(1)
	}

	p := prompt.BuildAsk(question, atlasOutput, i.String())

	if err := llm.Generate(p); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		os.Exit(1)
	}
}

func runForIntent(a atlas.Runner, entity string, i intent.Intent) (string, error) {
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

func runREPL(a atlas.Runner, llm ollama.LLM, conventions string) {
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
				solve.Run(a, llm, jiraText, conventions, false, "")
			}
		} else if strings.HasPrefix(input, "claude:") {
			jiraText := strings.TrimSpace(strings.TrimPrefix(input, "claude:"))
			if jiraText != "" {
				claude.Run(a, llm, jiraText, conventions, "claude-prompt.xml", "", false)
			}
		} else if strings.HasPrefix(input, "gen:") {
			desc := strings.TrimSpace(strings.TrimPrefix(input, "gen:"))
			if desc != "" {
				generate.Run(a, llm, desc, "", conventions)
			}
		} else {
			handleQuestion(a, llm, input)
		}
		fmt.Println()
	}
}
