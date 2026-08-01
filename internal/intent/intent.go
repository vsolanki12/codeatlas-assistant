package intent

import (
	"regexp"
	"strings"
)

type Intent int

const (
	Ask Intent = iota
	Explain
	Impact
	Investigate
	Search
	Stats
)

func (i Intent) String() string {
	switch i {
	case Explain:
		return "explain"
	case Impact:
		return "impact"
	case Investigate:
		return "investigate"
	case Search:
		return "search"
	case Stats:
		return "stats"
	default:
		return "ask"
	}
}

var intentKeywords = []struct {
	intent   Intent
	keywords []string
}{
	{Impact, []string{"impact", "break", "affect", "what breaks", "blast radius", "change"}},
	{Explain, []string{"explain", "how does", "how do", "work", "reconcil", "what does"}},
	{Investigate, []string{"everything", "investigate", "debug", "all about", "tell me about", "deep dive"}},
	{Search, []string{"search", "find", "where is", "list", "show me"}},
	{Stats, []string{"stats", "statistics", "count", "how many", "overview"}},
}

func Detect(question string) Intent {
	lower := strings.ToLower(question)
	for _, ik := range intentKeywords {
		for _, kw := range ik.keywords {
			if strings.Contains(lower, kw) {
				return ik.intent
			}
		}
	}
	return Ask
}

var stopWords = map[string]bool{
	"what": true, "how": true, "does": true, "do": true, "is": true,
	"the": true, "a": true, "an": true, "about": true, "of": true,
	"in": true, "for": true, "to": true, "me": true, "tell": true,
	"can": true, "you": true, "this": true, "that": true, "it": true,
	"if": true, "i": true, "my": true, "are": true, "was": true,
	"will": true, "would": true, "could": true, "should": true,
}

var intentWords = map[string]bool{
	"impact": true, "break": true, "affect": true, "breaks": true,
	"explain": true, "work": true, "works": true,
	"everything": true, "investigate": true, "debug": true,
	"search": true, "find": true, "list": true, "show": true,
	"stats": true, "statistics": true, "count": true,
	"blast": true, "radius": true, "change": true,
	"deep": true, "dive": true, "all": true,
}

func ExtractEntity(question string) string {
	words := strings.Fields(question)
	var kept []string
	for _, w := range words {
		clean := strings.Trim(strings.ToLower(w), "?!.,;:'\"")
		if stopWords[clean] || intentWords[clean] {
			continue
		}
		kept = append(kept, w)
	}
	return strings.Join(kept, " ")
}

var technicalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[A-Z][a-z]+(?:[A-Z][a-z]+)+`),
	regexp.MustCompile(`\b[a-z]+(?:[A-Z][a-z]+)+`),
	regexp.MustCompile(`[a-z]+_[a-z]+(?:_[a-z]+)*`),
	regexp.MustCompile(`[a-z]+\.[a-z]+\.io`),
	regexp.MustCompile(`[A-Z][a-zA-Z]*Reconciler`),
	regexp.MustCompile(`[A-Z][a-zA-Z]*Controller`),
	regexp.MustCompile(`(?i)reconcile[A-Z][a-zA-Z]*`),
	regexp.MustCompile(`(?:controller|crd|function):[a-zA-Z._]+`),
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
