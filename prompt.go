package main

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed conventions.md
var defaultConventions string

func loadConventions(path string) string {
	if path == "" {
		return defaultConventions
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "conventions file: %v (using default)\n", err)
		return defaultConventions
	}
	return string(data)
}

func BuildPrompt(question string, atlasOutput string) string {
	return fmt.Sprintf(`You are a senior Kubernetes/HyperShift engineer. Answer the question using ONLY the CodeAtlas data provided below. Be concise and technical. If the data doesn't contain the answer, say so.

## Question
%s

## CodeAtlas Data
%s

## Answer
`, question, atlasOutput)
}

func BuildSolvePrompt(jiraText string, atlasData string, conventions string, styleCode string) string {
	var conventionsSection string
	if conventions != "" {
		conventionsSection = fmt.Sprintf(`
## Engineering Conventions (FOLLOW THESE)
%s
`, conventions)
	}

	var styleSection string
	if styleCode != "" {
		styleSection = fmt.Sprintf(`
## Style Reference (existing code from this repo)
`+"```go\n"+styleCode+"\n```\n")
	}

	return fmt.Sprintf(`You are a senior Kubernetes/HyperShift engineer solving a JIRA issue.

Using the JIRA description, CodeAtlas architecture data, engineering conventions, and style reference below, provide:
1. **Root Cause**: What is likely causing this issue based on the code architecture
2. **Files to Change**: Which files need modification (with file paths from the atlas data)
3. **Approach**: Step-by-step fix, referencing specific functions, controllers, and following the conventions
4. **Feature Gate**: If this adds new API fields, which feature gate to use and how to wire it
5. **Tests**: What tests exist, what new tests are needed (follow the testing conventions)

Be specific. Reference actual controller names, function names, and file paths from the atlas data.
Follow the engineering conventions exactly — feature gates, enum types, testing patterns.

## JIRA Description
%s

## CodeAtlas Architecture Data
%s
%s%s
## Solution
`, jiraText, atlasData, conventionsSection, styleSection)
}

func BuildGeneratePrompt(description, atlasData, styleCode, conventions string) string {
	var conventionsSection string
	if conventions != "" {
		conventionsSection = fmt.Sprintf(`
## Engineering Conventions (FOLLOW THESE)
%s
`, conventions)
	}

	var styleSection string
	if styleCode != "" {
		styleSection = fmt.Sprintf(`
## Style Reference (MATCH THIS EXACTLY)
The code below is from the same codebase. Match its patterns exactly:
- Same import style and aliases
- Same error handling patterns (wrap with fmt.Errorf, use apierrors, etc.)
- Same logging style (ctrl.LoggerFrom vs log.FromContext)
- Same function signature patterns (context first, pointer receivers, etc.)
- Same condition/status update patterns
- Same naming conventions (camelCase for unexported, PascalCase for exported)

`+"```go\n"+styleCode+"\n```\n")
	}

	return fmt.Sprintf(`You are a senior Go/Kubernetes/HyperShift engineer writing production code.

Generate Go code that:
1. Follows the EXACT patterns from the style reference below (if provided)
2. Uses correct package names, function signatures, and types from the atlas data
3. Matches controller-runtime conventions from the existing codebase
4. Includes proper error handling matching the existing style
5. Follows the engineering conventions (feature gates, testing, API design)

Only output Go code with brief comments. No explanations outside code blocks.

## What to Generate
%s

## CodeAtlas Architecture Data (entities, relationships, file paths)
%s
%s%s
## Go Code
`, description, atlasData, conventionsSection, styleSection)
}
