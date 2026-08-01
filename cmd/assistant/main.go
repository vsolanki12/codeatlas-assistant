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
	claudeFlag := flag.String("claude", "", "generate Claude-optimized prompt from JIRA text")
	claudeFile := flag.String("claude-file", "", "generate Claude-optimized prompt from JIRA file")
	generateFlag := flag.String("generate", "", "generate Go code (describe what to write)")
	styleFile := flag.String("style-file", "", "Go file to use as style reference (auto-detect if empty)")
	conventionsFile := flag.String("conventions", "", "conventions file (embedded default if empty)")
	forceSolve := flag.Bool("force-solve", false, "skip existing fix check in solve mode")
	flag.Parse()

	resolvedModel, err := resolveModel(*model)
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
		claude.Run(a, llm, string(data), conventions)
		return
	}

	if *claudeFlag != "" {
		claude.Run(a, llm, *claudeFlag, conventions)
		return
	}

	if *solveFile != "" {
		data, err := os.ReadFile(*solveFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			os.Exit(1)
		}
		solve.Run(a, llm, string(data), conventions, *forceSolve)
		return
	}

	if *solveFlag != "" {
		solve.Run(a, llm, *solveFlag, conventions, *forceSolve)
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
		fmt.Fprintln(os.Stderr, "       assistant --claude \"JIRA description text\"")
		fmt.Fprintln(os.Stderr, "       assistant --claude-file jira.txt")
		fmt.Fprintln(os.Stderr, "       assistant --generate \"add a validation function for NodePool\"")
		fmt.Fprintln(os.Stderr, "       assistant --interactive")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags: --model name, --graph path, --conventions file")
		os.Exit(1)
	}

	handleQuestion(a, llm, question)
}

func resolveModel(model string) (string, error) {
	if model != "" {
		return model, nil
	}

	models, err := ollama.ListModels()
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no ollama models installed — run: ollama pull qwen3:8b")
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

	p := prompt.BuildAsk(question, atlasOutput)

	if err := llm.Generate(p); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		os.Exit(1)
	}
}

func runForIntent(a atlas.Runner, entity string, i intent.Intent) (string, error) {
	switch i {
	case intent.Explain:
		return a.Run("explain", entity)
	case intent.Impact:
		return a.Run("impact", entity)
	case intent.Investigate:
		return a.Run("investigate", entity)
	case intent.Search:
		return a.Run("search", entity)
	case intent.Stats:
		return a.Run("stats")
	default:
		return a.Run("ask", entity)
	}
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
				solve.Run(a, llm, jiraText, conventions, false)
			}
		} else if strings.HasPrefix(input, "claude:") {
			jiraText := strings.TrimSpace(strings.TrimPrefix(input, "claude:"))
			if jiraText != "" {
				claude.Run(a, llm, jiraText, conventions)
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
