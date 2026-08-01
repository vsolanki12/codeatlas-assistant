package main

import "strings"

type Intent int

const (
	IntentAsk Intent = iota
	IntentExplain
	IntentImpact
	IntentInvestigate
	IntentSearch
	IntentStats
)

func (i Intent) String() string {
	switch i {
	case IntentExplain:
		return "explain"
	case IntentImpact:
		return "impact"
	case IntentInvestigate:
		return "investigate"
	case IntentSearch:
		return "search"
	case IntentStats:
		return "stats"
	default:
		return "ask"
	}
}

var intentKeywords = []struct {
	intent   Intent
	keywords []string
}{
	{IntentImpact, []string{"impact", "break", "affect", "what breaks", "blast radius", "change"}},
	{IntentExplain, []string{"explain", "how does", "how do", "work", "reconcil", "what does"}},
	{IntentInvestigate, []string{"everything", "investigate", "debug", "all about", "tell me about", "deep dive"}},
	{IntentSearch, []string{"search", "find", "where is", "list", "show me"}},
	{IntentStats, []string{"stats", "statistics", "count", "how many", "overview"}},
}

func DetectIntent(question string) Intent {
	lower := strings.ToLower(question)
	for _, ik := range intentKeywords {
		for _, kw := range ik.keywords {
			if strings.Contains(lower, kw) {
				return ik.intent
			}
		}
	}
	return IntentAsk
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
