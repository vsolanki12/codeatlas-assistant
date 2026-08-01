package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	model := flag.String("model", "", "ollama model name (auto-detect if empty)")
	graphPath := flag.String("graph", "atlas.json", "path to atlas graph JSON")
	interactive := flag.Bool("interactive", false, "interactive REPL mode")
	solve := flag.String("solve", "", "solve a JIRA issue (pass description text)")
	solveFile := flag.String("solve-file", "", "solve a JIRA issue (read description from file)")
	generate := flag.String("generate", "", "generate Go code (describe what to write)")
	styleFile := flag.String("style-file", "", "Go file to use as style reference (auto-detect if empty)")
	flag.Parse()

	resolvedModel, err := resolveModel(*model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *solveFile != "" {
		data, err := os.ReadFile(*solveFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			os.Exit(1)
		}
		Solve(resolvedModel, *graphPath, string(data))
		return
	}

	if *solve != "" {
		Solve(resolvedModel, *graphPath, *solve)
		return
	}

	if *generate != "" {
		Generate(resolvedModel, *graphPath, *generate, *styleFile)
		return
	}

	if *interactive {
		runREPL(resolvedModel, *graphPath)
		return
	}

	question := strings.Join(flag.Args(), " ")
	if question == "" {
		fmt.Fprintln(os.Stderr, "usage: assistant [flags] \"your question\"")
		fmt.Fprintln(os.Stderr, "       assistant --solve \"JIRA description text\"")
		fmt.Fprintln(os.Stderr, "       assistant --solve-file jira.txt")
		fmt.Fprintln(os.Stderr, "       assistant --generate \"add a validation function for NodePool\"")
		fmt.Fprintln(os.Stderr, "       assistant --interactive")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags: --model name, --graph path")
		os.Exit(1)
	}

	handleQuestion(resolvedModel, *graphPath, question)
}

func resolveModel(model string) (string, error) {
	if model != "" {
		return model, nil
	}

	models, err := ListModels()
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no ollama models installed — run: ollama pull qwen3:8b")
	}

	fmt.Fprintf(os.Stderr, "using model: %s\n", models[0])
	return models[0], nil
}

func handleQuestion(model, graphPath, question string) {
	intent := DetectIntent(question)
	entity := ExtractEntity(question)

	fmt.Fprintf(os.Stderr, "intent: %s | entity: %q\n", intent, entity)

	atlasOutput, err := RunForIntent(graphPath, entity, intent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atlas error: %v\n", err)
		os.Exit(1)
	}

	prompt := BuildPrompt(question, atlasOutput)

	if err := AskOllamaStream(model, prompt); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		os.Exit(1)
	}
}

func runREPL(model, graphPath string) {
	fmt.Println("CodeAtlas Assistant (type 'exit' to quit)")
	fmt.Println("  prefix with 'solve:' to analyze a JIRA description")
	fmt.Println("  prefix with 'gen:' to generate Go code")
	fmt.Printf("  graph: %s | model: %s\n", graphPath, model)
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
				Solve(model, graphPath, jiraText)
			}
		} else if strings.HasPrefix(input, "gen:") {
			desc := strings.TrimSpace(strings.TrimPrefix(input, "gen:"))
			if desc != "" {
				Generate(model, graphPath, desc, "")
			}
		} else {
			handleQuestion(model, graphPath, input)
		}
		fmt.Println()
	}
}
