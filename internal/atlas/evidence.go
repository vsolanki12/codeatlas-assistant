package atlas

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const EvidenceVersion = "1.0"
const DefaultEvidenceBudget = 16384
const DefaultSourceBudget = 24000

// EvidencePacket is the versioned selection manifest produced by Atlas. The
// assistant materializes these spans; it does not repeat Atlas's ranking or
// infer additional repository relationships.
type EvidencePacket struct {
	Version          string            `json:"version"`
	Status           string            `json:"status"`
	Graph            json.RawMessage   `json:"graph"`
	GraphFingerprint string            `json:"graphFingerprint"`
	Entities         []EvidenceEntity  `json:"entities"`
	Relationships    []RelationshipRef `json:"relationships"`
	Sources          []EvidenceSource  `json:"sources"`
	Candidates       []EvidenceEntity  `json:"candidates,omitempty"`
	Omissions        []json.RawMessage `json:"omissions,omitempty"`
	BudgetBytes      int               `json:"budgetBytes"`
	Total            int               `json:"total"`
	NextOffset       *int              `json:"nextOffset,omitempty"`
	Truncated        bool              `json:"truncated"`
}

type EvidenceEntity struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Package     string     `json:"package,omitempty"`
	Description string     `json:"description,omitempty"`
	Source      SourceSpan `json:"source"`
	Score       int        `json:"score"`
	Reasons     []string   `json:"reasons"`
}

type SourceSpan struct {
	Parser  string `json:"parser"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	EndLine int    `json:"endLine,omitempty"`
}

type EvidenceSource struct {
	EntityID  string     `json:"entityID"`
	Role      string     `json:"role"`
	Source    SourceSpan `json:"source"`
	FocusLine int        `json:"focusLine,omitempty"`
	Reason    string     `json:"reason"`
}

type EvidenceRequest struct {
	Question    string
	Entity      string
	Scope       string
	Intent      string
	BudgetBytes int
}

// QueryEvidence fails closed on incompatible Atlas versions and retrieval
// failures. No fallback discards the question or silently guesses an entity.
func QueryEvidence(a Runner, request EvidenceRequest) (*EvidencePacket, string, error) {
	budget := request.BudgetBytes
	if budget == 0 {
		budget = EvidenceBudget(a)
	}
	args := []string{"evidence", "--question", request.Question, "--intent", request.Intent, "--budget-bytes", strconv.Itoa(budget)}
	if request.Entity != "" {
		args = append(args, "--entity", request.Entity)
	}
	if request.Scope != "" {
		args = append(args, "--scope", request.Scope)
	}
	var output string
	var err error
	if runner, ok := a.(JSONRunner); ok {
		output, err = runner.RunJSON(args...)
	} else {
		output, err = a.Run(append(args, "--json")...)
	}
	if err != nil {
		return nil, "", fmt.Errorf("Atlas evidence query: %w", err)
	}
	packet, err := ParseEvidence(output)
	if err != nil {
		return nil, "", err
	}
	if err := packet.RequireEvidence(); err != nil {
		return packet, output, err
	}
	return packet, output, nil
}

func ParseEvidence(data string) (*EvidencePacket, error) {
	var packet EvidencePacket
	if err := json.Unmarshal([]byte(data), &packet); err != nil {
		return nil, fmt.Errorf("invalid Atlas evidence packet: %w", err)
	}
	if packet.Version != EvidenceVersion {
		return nil, fmt.Errorf("incompatible Atlas evidence version %q; expected %s", packet.Version, EvidenceVersion)
	}
	switch packet.Status {
	case "ok", "no_match", "ambiguous", "budget_exhausted":
	default:
		return nil, fmt.Errorf("unsupported Atlas evidence status %q", packet.Status)
	}
	if strings.TrimSpace(packet.GraphFingerprint) == "" {
		return nil, fmt.Errorf("Atlas evidence packet has no graph fingerprint")
	}
	entities := make(map[string]bool, len(packet.Entities))
	for _, entity := range packet.Entities {
		if entity.ID == "" || entity.Kind == "" {
			return nil, fmt.Errorf("Atlas evidence contains an invalid entity")
		}
		entities[entity.ID] = true
	}
	for _, source := range packet.Sources {
		if !entities[source.EntityID] || source.Source.File == "" || source.Source.Line < 1 || (source.Source.EndLine > 0 && source.Source.EndLine < source.Source.Line) {
			return nil, fmt.Errorf("Atlas evidence contains an invalid source span for %q", source.EntityID)
		}
		end := source.Source.EndLine
		if end == 0 {
			end = source.Source.Line
		}
		if source.FocusLine != 0 && (source.FocusLine < source.Source.Line || source.FocusLine > end) {
			return nil, fmt.Errorf("Atlas evidence has a focus outside its source span for %q", source.EntityID)
		}
	}
	return &packet, nil
}

func EvidenceBudget(a Runner) int {
	if configured, ok := a.(interface{ ContextBudgets() (int, int) }); ok {
		if evidence, _ := configured.ContextBudgets(); evidence > 0 {
			return evidence
		}
	}
	return DefaultEvidenceBudget
}

func SourceBudget(a Runner) int {
	if configured, ok := a.(interface{ ContextBudgets() (int, int) }); ok {
		if _, source := configured.ContextBudgets(); source > 0 {
			return source
		}
	}
	return DefaultSourceBudget
}

func (p *EvidencePacket) RequireEvidence() error {
	if p.Status != "ok" {
		return fmt.Errorf("Atlas evidence retrieval returned %s; refine the question, provide an exact entity ID, or increase the budget", p.Status)
	}
	if len(p.Entities) == 0 || len(p.Sources) == 0 {
		return fmt.Errorf("Atlas evidence contains no source-backed matches")
	}
	return nil
}

func (p *EvidencePacket) Repository() string {
	var graph struct {
		Repository string `json:"repository"`
	}
	_ = json.Unmarshal(p.Graph, &graph)
	return graph.Repository
}

// EntityLabel names an entity within this packet. PromptEvidence supplies the
// exact-ID legend once, so excerpts and relationships can refer to it cheaply.
func (p *EvidencePacket) EntityLabel(id string) string {
	for i, entity := range p.Entities {
		if entity.ID == id {
			return "E" + strconv.Itoa(i+1)
		}
	}
	return id
}

// PromptEvidence projects the validated manifest for a model. Selection scores
// and repeated JSON keys are not repository facts. Provenance, exact identities,
// relationship confidence/proof, and omissions remain in the prompt; the
// materializer supplies the actual retained source spans separately.
func (p *EvidencePacket) PromptEvidence() string {
	var out strings.Builder
	fmt.Fprintf(&out, "CodeAtlas evidence %s; status: %s; graphFingerprint: %s; truncated: %t\n", p.Version, p.Status, p.GraphFingerprint, p.Truncated)
	fmt.Fprintf(&out, "Graph provenance and extraction coverage: %s\n", p.Graph)
	out.WriteString("Entity legend (labels refer to these exact IDs):\n")
	for _, entity := range p.Entities {
		end := entity.Source.EndLine
		if end == 0 {
			end = entity.Source.Line
		}
		fmt.Fprintf(&out, "%s | %s | %s | %s:%d-%d\n", p.EntityLabel(entity.ID), entity.ID, entity.Name, entity.Source.File, entity.Source.Line, end)
		if entity.Description != "" {
			fmt.Fprintf(&out, "  description: %s\n", entity.Description)
		}
	}
	out.WriteString("Relationships (static evidence; tested_by does not prove behavioral coverage):\n")
	for _, edge := range p.Relationships {
		fmt.Fprintf(&out, "%s --%s--> %s (%s); %s %s:%d; %s; %s\n", p.EntityLabel(edge.From), edge.Type, p.EntityLabel(edge.To), edge.Confidence, edge.Evidence.Parser, edge.Evidence.File, edge.Evidence.Line, edge.Evidence.Snippet, edge.Evidence.Reason)
	}
	for _, omission := range p.Omissions {
		fmt.Fprintf(&out, "Selection omission: %s\n", omission)
	}
	return out.String()
}
