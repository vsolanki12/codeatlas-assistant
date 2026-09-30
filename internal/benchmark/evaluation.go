package benchmark

import (
	"fmt"
	"strings"

	"github.com/vsolanki12/codeatlas-assistant/internal/atlas"
	"github.com/vsolanki12/codeatlas-assistant/internal/metrics"
	"github.com/vsolanki12/codeatlas-assistant/internal/prompt"
	"github.com/vsolanki12/codeatlas-assistant/internal/workingset"
)

// Task describes a retrieval requirement, not an expected model-written
// answer. Fixtures must name evidence independently of the candidate ranking.
type Task struct {
	ID                string               `json:"id"`
	Question          string               `json:"question"`
	Entity            string               `json:"entity,omitempty"`
	Scope             string               `json:"scope,omitempty"`
	Intent            string               `json:"intent,omitempty"`
	ExpectedStatus    string               `json:"expectedStatus,omitempty"`
	RequiredEntities  []string             `json:"requiredEntities,omitempty"`
	RequiredSources   []SourceRequirement  `json:"requiredSources,omitempty"`
	RequiredSnippets  []SnippetRequirement `json:"requiredSnippets,omitempty"`
	ForbiddenEntities []string             `json:"forbiddenEntities,omitempty"`
}

type SourceRequirement struct {
	File    string `json:"file"`
	Role    string `json:"role,omitempty"`
	Line    int    `json:"line"`
	EndLine int    `json:"endLine,omitempty"`
}

type SnippetRequirement struct {
	Contains string `json:"contains"`
}

type TaskResult struct {
	TaskID            string       `json:"taskID"`
	BudgetBytes       int          `json:"budgetBytes"`
	SourceBudgetBytes int          `json:"sourceBudgetBytes"`
	Status            string       `json:"status"`
	Passed            bool         `json:"passed"`
	Failures          []string     `json:"failures,omitempty"`
	AtlasCalls        int          `json:"atlasCalls"`
	Manifest          metrics.Text `json:"manifest"`
	PromptEvidence    metrics.Text `json:"promptEvidence"`
	SourceContext     metrics.Text `json:"sourceContext"`
	Prompt            metrics.Text `json:"prompt"`
	UniqueFiles       int          `json:"uniqueFiles"`
	UniqueSourceLines int          `json:"uniqueSourceLines"`
	Truncated         bool         `json:"truncated"`
	GraphFingerprint  string       `json:"graphFingerprint,omitempty"`
}

// Evaluate runs task fixtures under each requested packet budget without an
// LLM. Actual model/provider token counts are deliberately absent. Prompt
// estimates measure the complete packet plus materialized source context.
func Evaluate(a atlas.Runner, tasks []Task, budgets []int, repoPath string) ([]TaskResult, error) {
	if len(tasks) == 0 || len(budgets) == 0 {
		return nil, fmt.Errorf("task fixtures and budgets are required")
	}
	if freshness := atlas.CheckFreshness(a, repoPath); freshness.BlocksImplementation() {
		return nil, fmt.Errorf("cannot evaluate unverified graph: %s", freshness.Warning())
	}
	var results []TaskResult
	for _, task := range tasks {
		if task.ID == "" || strings.TrimSpace(task.Question+task.Entity) == "" {
			return nil, fmt.Errorf("task fixtures require an id and question or entity")
		}
		intent := task.Intent
		if intent == "" {
			intent = "understand"
		}
		expected := task.ExpectedStatus
		if expected == "" {
			expected = "ok"
		}
		switch expected {
		case "ok", "no_match", "ambiguous", "budget_exhausted":
		default:
			return nil, fmt.Errorf("task %s has unsupported expected status %q", task.ID, expected)
		}
		for _, required := range task.RequiredSources {
			if required.File == "" || required.Line < 1 || (required.EndLine > 0 && required.EndLine < required.Line) {
				return nil, fmt.Errorf("task %s has an invalid required source span", task.ID)
			}
		}
		for _, required := range task.RequiredSnippets {
			if strings.TrimSpace(required.Contains) == "" {
				return nil, fmt.Errorf("task %s has an empty required snippet", task.ID)
			}
		}
		if expected == "ok" && len(task.RequiredEntities)+len(task.RequiredSources)+len(task.RequiredSnippets) == 0 {
			return nil, fmt.Errorf("task %s has no correctness requirements", task.ID)
		}
		for _, budget := range budgets {
			if budget < 2048 {
				return nil, fmt.Errorf("benchmark budgets must be at least 2048 bytes")
			}
			packet, data, queryErr := atlas.QueryEvidence(a, atlas.EvidenceRequest{Question: task.Question, Entity: task.Entity, Scope: task.Scope, Intent: intent, BudgetBytes: budget})
			result := TaskResult{TaskID: task.ID, BudgetBytes: budget, SourceBudgetBytes: atlas.SourceBudget(a), AtlasCalls: 1, Manifest: metrics.Measure(data)}
			if packet == nil {
				result.Failures = append(result.Failures, fmt.Sprint(queryErr))
				results = append(results, result)
				continue
			}
			result.Status, result.GraphFingerprint, result.Truncated = packet.Status, packet.GraphFingerprint, packet.Truncated
			if result.Status != expected {
				result.Failures = append(result.Failures, fmt.Sprintf("status %s; expected %s", result.Status, expected))
			}
			ids := make(map[string]bool)
			for _, entity := range packet.Entities {
				ids[entity.ID] = true
			}
			for _, id := range task.RequiredEntities {
				if !ids[id] {
					result.Failures = append(result.Failures, "missing entity "+id)
				}
			}
			for _, id := range task.ForbiddenEntities {
				if ids[id] {
					result.Failures = append(result.Failures, "unexpected entity "+id)
				}
			}
			if packet.Status == "ok" {
				ws, err := workingset.FromEvidence(repoPath, packet, atlas.SourceBudget(a))
				if err != nil {
					result.Failures = append(result.Failures, err.Error())
				} else {
					context := ws.PromptContext()
					materializedCode := ws.MaterializedCode()
					result.SourceContext = metrics.Measure(context)
					result.PromptEvidence = metrics.Measure(packet.PromptEvidence())
					promptIntent := "explain"
					if intent == "debug" {
						promptIntent = "investigate"
					}
					if intent == "impact" {
						promptIntent = "impact"
					}
					result.Prompt = metrics.Measure(prompt.BuildAsk(task.Question, packet.PromptEvidence()+"\n\n"+context, promptIntent))
					result.Truncated = result.Truncated || len(ws.Omissions) > 0
					lines := make(map[string]bool)
					files := make(map[string]bool)
					for _, source := range ws.Sources {
						files[source.Source.File] = true
						for line := source.Source.Line; line <= source.Source.EndLine; line++ {
							lines[fmt.Sprintf("%s:%d", source.Source.File, line)] = true
						}
					}
					result.UniqueFiles, result.UniqueSourceLines = len(files), len(lines)
					for _, required := range task.RequiredSources {
						found := false
						end := required.EndLine
						if end == 0 {
							end = required.Line
						}
						for _, source := range ws.Sources {
							if source.Source.File == required.File && (required.Role == "" || source.Role == required.Role) && source.Source.Line <= required.Line && source.Source.EndLine >= end {
								found = true
								break
							}
						}
						if !found {
							result.Failures = append(result.Failures, fmt.Sprintf("missing materialized source %s:%d-%d (%s)", required.File, required.Line, end, required.Role))
						}
					}
					for _, required := range task.RequiredSnippets {
						if !strings.Contains(materializedCode, required.Contains) {
							result.Failures = append(result.Failures, "missing source snippet "+required.Contains)
						}
					}
				}
			} else if len(task.RequiredSources)+len(task.RequiredSnippets) > 0 {
				result.Failures = append(result.Failures, "retrieval failure left required source unmaterialized")
			}
			result.Passed = len(result.Failures) == 0
			results = append(results, result)
		}
	}
	return results, nil
}
