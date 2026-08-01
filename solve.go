package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var technicalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[A-Z][a-z]+(?:[A-Z][a-z]+)+`),            // CamelCase: HostedCluster, NodePool
	regexp.MustCompile(`\b[a-z]+(?:[A-Z][a-z]+)+`),                // camelCase: reconcileEtcd, deleteNodePools
	regexp.MustCompile(`[a-z]+_[a-z]+(?:_[a-z]+)*`),              // snake_case: hosted_cluster
	regexp.MustCompile(`[a-z]+\.[a-z]+\.io`),                     // CRD groups: hypershift.openshift.io
	regexp.MustCompile(`[A-Z][a-zA-Z]*Reconciler`),               // Reconciler names
	regexp.MustCompile(`[A-Z][a-zA-Z]*Controller`),               // Controller names
	regexp.MustCompile(`(?i)reconcile[A-Z][a-zA-Z]*`),            // reconcileXxx functions
	regexp.MustCompile(`(?:controller|crd|function):[a-zA-Z._]+`), // atlas entity IDs
}

var jiraStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "shall": true, "can": true, "need": true,
	"to": true, "of": true, "in": true, "for": true, "on": true,
	"at": true, "by": true, "with": true, "from": true, "as": true,
	"into": true, "through": true, "during": true, "before": true,
	"after": true, "above": true, "below": true, "between": true,
	"and": true, "but": true, "or": true, "nor": true, "not": true,
	"so": true, "yet": true, "both": true, "either": true, "neither": true,
	"each": true, "every": true, "all": true, "any": true, "few": true,
	"more": true, "most": true, "other": true, "some": true, "such": true,
	"no": true, "only": true, "own": true, "same": true, "than": true,
	"too": true, "very": true, "just": true, "because": true, "when": true,
	"while": true, "if": true, "then": true, "else": true, "also": true,
	"that": true, "this": true, "these": true, "those": true, "it": true,
	"its": true, "we": true, "they": true, "them": true, "their": true,
	"i": true, "me": true, "my": true, "you": true, "your": true,
	"he": true, "she": true, "his": true, "her": true,
}

func ExtractTechnicalTerms(text string) []string {
	seen := make(map[string]bool)
	var terms []string

	for _, pat := range technicalPatterns {
		matches := pat.FindAllString(text, -1)
		for _, m := range matches {
			lower := strings.ToLower(m)
			if !seen[lower] {
				seen[lower] = true
				terms = append(terms, m)
			}
		}
	}

	words := strings.Fields(text)
	for _, w := range words {
		clean := strings.Trim(w, "?!.,;:'\"()[]{}*`~")
		lower := strings.ToLower(clean)
		if len(clean) < 3 || jiraStopWords[lower] || seen[lower] {
			continue
		}
		if strings.Contains(clean, "/") || strings.Contains(clean, ".go") ||
			strings.Contains(clean, "CRD") || strings.Contains(clean, "API") ||
			(len(clean) > 0 && clean[0] >= 'A' && clean[0] <= 'Z' && len(clean) > 3) {
			if !seen[lower] {
				seen[lower] = true
				terms = append(terms, clean)
			}
		}
	}

	return terms
}

func Solve(model, graphPath, jiraText string) {
	fmt.Fprintln(os.Stderr, "--- Extracting technical terms ---")
	terms := ExtractTechnicalTerms(jiraText)

	if len(terms) == 0 {
		fmt.Fprintln(os.Stderr, "no technical terms found in JIRA text")
		return
	}

	if len(terms) > 8 {
		terms = terms[:8]
	}

	fmt.Fprintf(os.Stderr, "terms: %s\n", strings.Join(terms, ", "))

	fmt.Fprintln(os.Stderr, "--- Searching atlas ---")
	var atlasData strings.Builder

	for _, term := range terms {
		result, err := runAtlas(graphPath, "search", term)
		if err != nil || strings.Contains(result, "No matching") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Search: %s\n%s\n", term, result))
	}

	if atlasData.Len() == 0 {
		fmt.Fprintln(os.Stderr, "no atlas matches found")
		prompt := BuildSolvePrompt(jiraText, "(no atlas data available)")
		if err := AskOllamaStream(model, prompt); err != nil {
			fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		}
		return
	}

	topTerm := terms[0]
	fmt.Fprintf(os.Stderr, "--- Deep dive: %s ---\n", topTerm)

	explainResult, err := runAtlas(graphPath, "explain", topTerm)
	if err == nil && !strings.Contains(explainResult, "not found") {
		atlasData.WriteString(fmt.Sprintf("### Explain: %s\n%s\n", topTerm, explainResult))
	}

	investigateResult, err := runAtlas(graphPath, "investigate", topTerm)
	if err == nil && !strings.Contains(investigateResult, "not found") {
		atlasData.WriteString(fmt.Sprintf("### Investigate: %s\n%s\n", topTerm, investigateResult))
	}

	fmt.Fprintln(os.Stderr, "--- Generating solution ---")
	prompt := BuildSolvePrompt(jiraText, atlasData.String())

	if err := AskOllamaStream(model, prompt); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
	}
}
