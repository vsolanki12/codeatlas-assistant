package main

import "fmt"

func BuildPrompt(question string, atlasOutput string) string {
	return fmt.Sprintf(`You are a senior Kubernetes/HyperShift engineer. Answer the question using ONLY the CodeAtlas data provided below. Be concise and technical. If the data doesn't contain the answer, say so.

## Question
%s

## CodeAtlas Data
%s

## Answer
`, question, atlasOutput)
}

func BuildSolvePrompt(jiraText string, atlasData string) string {
	return fmt.Sprintf(`You are a senior Kubernetes/HyperShift engineer solving a JIRA issue.

Using the JIRA description and CodeAtlas architecture data below, provide:
1. **Root Cause**: What is likely causing this issue based on the code architecture
2. **Files to Change**: Which files need modification (with file paths from the atlas data)
3. **Approach**: Step-by-step fix, referencing specific functions and controllers
4. **Tests**: What tests exist and what new tests are needed

Be specific. Reference actual controller names, function names, and file paths from the atlas data.

## JIRA Description
%s

## CodeAtlas Architecture Data
%s

## Solution
`, jiraText, atlasData)
}
