package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var entityIDPattern = regexp.MustCompile(`(?:controller|function|crd|package|test|document):[a-zA-Z0-9._]+`)

var technicalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[A-Z][a-z]+(?:[A-Z][a-z]+)+`),            // CamelCase: HostedCluster, NodePool
	regexp.MustCompile(`\b[a-z]+(?:[A-Z][a-z]+)+`),               // camelCase: reconcileEtcd, deleteNodePools
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

func Solve(model, graphPath, jiraText, conventions string) {
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
		prompt := BuildSolvePrompt(jiraText, "(no atlas data available)", conventions, "")
		if err := AskOllamaStream(model, prompt); err != nil {
			fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
		}
		return
	}

	deepDiveCount := 3
	if len(terms) < deepDiveCount {
		deepDiveCount = len(terms)
	}

	for i := 0; i < deepDiveCount; i++ {
		term := terms[i]
		fmt.Fprintf(os.Stderr, "--- Deep dive: %s ---\n", term)

		explainResult, err := runAtlas(graphPath, "explain", term)
		if err == nil && !strings.Contains(explainResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Explain: %s\n%s\n", term, explainResult))
		}

		investigateResult, err := runAtlas(graphPath, "investigate", term)
		if err == nil && !strings.Contains(investigateResult, "not found") {
			atlasData.WriteString(fmt.Sprintf("### Investigate: %s\n%s\n", term, investigateResult))
		}
	}

	expandRelatedEntities(graphPath, terms, &atlasData)

	styleCode := loadStyleReference("", atlasData.String(), graphPath)
	if styleCode != "" {
		fmt.Fprintln(os.Stderr, "--- Style reference loaded ---")
	}

	fmt.Fprintln(os.Stderr, "--- Generating solution ---")
	prompt := BuildSolvePrompt(jiraText, atlasData.String(), conventions, styleCode)

	if err := AskOllamaStream(model, prompt); err != nil {
		fmt.Fprintf(os.Stderr, "ollama error: %v\n", err)
	}
}

func expandRelatedEntities(graphPath string, searchedTerms []string, atlasData *strings.Builder) {
	collected := atlasData.String()
	entityIDs := entityIDPattern.FindAllString(collected, -1)
	if len(entityIDs) == 0 {
		return
	}

	searched := make(map[string]bool)
	for _, t := range searchedTerms {
		searched[strings.ToLower(t)] = true
	}

	var novel []string
	seen := make(map[string]bool)
	for _, id := range entityIDs {
		if seen[id] || searched[strings.ToLower(id)] {
			continue
		}
		seen[id] = true
		parts := strings.SplitN(id, ":", 2)
		if len(parts) == 2 && !searched[strings.ToLower(parts[1])] {
			novel = append(novel, id)
		}
	}

	if len(novel) == 0 {
		return
	}

	if len(novel) > 5 {
		novel = novel[:5]
	}

	fmt.Fprintf(os.Stderr, "--- Expanding %d related entities ---\n", len(novel))
	for _, id := range novel {
		parts := strings.SplitN(id, ":", 2)
		name := parts[1]
		if idx := strings.LastIndex(name, "."); idx != -1 {
			name = name[idx+1:]
		}

		result, err := runAtlas(graphPath, "investigate", name)
		if err != nil || strings.Contains(result, "not found") {
			continue
		}
		atlasData.WriteString(fmt.Sprintf("### Related: %s\n%s\n", id, result))
		fmt.Fprintf(os.Stderr, "  expanded: %s\n", id)
	}
}
