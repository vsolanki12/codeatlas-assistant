package intent

import (
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name     string
		question string
		want     Intent
	}{
		{"explain via how does", "how does HostedClusterReconciler work", Explain},
		{"impact via break", "what breaks if I change NodePool", Impact},
		{"impact via what breaks phrase", "what breaks when I modify the CRD", Impact},
		{"impact via change keyword", "change the NodePool spec", Impact},
		{"investigate keyword", "investigate the auth flow", Investigate},
		{"investigate via tell me about", "tell me about HostedCluster", Investigate},
		{"investigate via debug", "debug the auth failure", Investigate},
		{"search via where is", "where is the config directory", Search},
		{"search via find", "find all controllers", Search},
		{"stats via how many", "how many controllers are there", Stats},
		{"stats via count", "count the packages", Stats},
		{"default ask bare entity", "HostedCluster", Ask},
		{"default ask empty string", "", Ask},
		{"priority explain before investigate", "explain everything about reconcile", Explain},
		{"explain via reconcil substring", "reconcileHostedCluster logic", Explain},
		{"case insensitive", "HOW DOES this WORK", Explain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Detect(tt.question)
			if got != tt.want {
				t.Errorf("Detect(%q) = %v, want %v", tt.question, got, tt.want)
			}
		})
	}
}

func TestIntentString(t *testing.T) {
	tests := []struct {
		intent Intent
		want   string
	}{
		{Ask, "ask"},
		{Explain, "explain"},
		{Impact, "impact"},
		{Investigate, "investigate"},
		{Search, "search"},
		{Stats, "stats"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.intent.String(); got != tt.want {
				t.Errorf("Intent(%d).String() = %q, want %q", tt.intent, got, tt.want)
			}
		})
	}
}

func TestExtractEntity(t *testing.T) {
	tests := []struct {
		name     string
		question string
		want     string
	}{
		{"CamelCase pattern", "how does HostedClusterReconciler work", "HostedClusterReconciler"},
		{"camelCase pattern", "how does reconcileAuth work", "reconcileAuth"},
		{"snake_case pattern", "what is node_pool_controller", "node_pool_controller"},
		{"stop and intent words stripped", "tell me about something simple", "something simple"},
		{"bare CamelCase entity", "HostedClusterReconciler", "HostedClusterReconciler"},
		{"all stop and intent words", "how does it work", ""},
		{"empty string", "", ""},
		{"punctuation trimmed", "what is HostedCluster?", "HostedCluster"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEntity(tt.question)
			if got != tt.want {
				t.Errorf("ExtractEntity(%q) = %q, want %q", tt.question, got, tt.want)
			}
		})
	}
}

func TestExtractTechnicalTerms(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		wantContains []string
		wantEmpty    bool
	}{
		{
			name:         "parenthetical expansion and acronym",
			text:         "Hosted Control Plane (HCP) manages clusters",
			wantContains: []string{"HostedControlPlane", "HCP"},
		},
		{
			name:         "CamelCase and snake_case from file path",
			text:         "NodePoolReconciler in hostedcluster_controller.go",
			wantContains: []string{"NodePoolReconciler", "hostedcluster_controller"},
		},
		{
			name:         "acronyms extracted",
			text:         "the AWS and AZURE providers",
			wantContains: []string{"AWS", "AZURE"},
		},
		{
			name:      "empty string",
			text:      "",
			wantEmpty: true,
		},
		{
			name:         "mixed camelCase snake_case and acronym",
			text:         "reconcileNodePool calls node_pool_manager for AWS",
			wantContains: []string{"reconcileNodePool", "node_pool_manager", "AWS"},
		},
		{
			name:         "file path with .go suffix",
			text:         "look at hostedcluster_controller.go",
			wantContains: []string{"hostedcluster_controller.go"},
		},
		{
			name:         "Reconciler suffix pattern",
			text:         "the HostedClusterReconciler runs",
			wantContains: []string{"HostedClusterReconciler"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTechnicalTerms(tt.text)
			if tt.wantEmpty {
				if len(got) != 0 {
					t.Errorf("ExtractTechnicalTerms(%q) = %v, want empty", tt.text, got)
				}
				return
			}
			for _, want := range tt.wantContains {
				if !containsTerm(got, want) {
					t.Errorf("ExtractTechnicalTerms(%q) missing %q, got %v", tt.text, want, got)
				}
			}
		})
	}
}

func containsTerm(terms []string, target string) bool {
	for _, t := range terms {
		if t == target {
			return true
		}
	}
	return false
}
