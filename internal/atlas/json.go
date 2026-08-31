package atlas

import (
	"encoding/json"
	"sort"
	"strings"
)

// EntityRef is the small portion of a CodeAtlas entity that assistant
// workflows need for routing and file selection. It deliberately does not
// duplicate the graph model or attempt to interpret repository structure.
type EntityRef struct {
	ID     string
	Name   string
	Kind   string
	Source struct {
		File    string
		Line    int
		EndLine int
	}
	Files []string
}

// RelationshipRef is the small, evidence-bearing relationship shape used by
// assistant routing. It lets consumers apply deterministic rules to graph
// edges without reimplementing repository analysis.
type RelationshipRef struct {
	ID         string
	From       string
	To         string
	Type       string
	Confidence string
	Evidence   struct {
		Parser  string
		File    string
		Line    int
		Reason  string
		Snippet string
	}
}

// EntityRefs extracts entity objects from a JSON query result. It handles
// search, investigate, impact, explain, and ask envelopes without depending
// on their individual nesting. Text output remains supported by callers as a
// compatibility path for older Atlas binaries.
func EntityRefs(data string) []EntityRef {
	seen := make(map[string]bool)
	var refs []EntityRef
	var walk func(any)
	walk = func(current any) {
		switch value := current.(type) {
		case []any:
			for _, item := range value {
				walk(item)
			}
		case map[string]any:
			id, _ := value["id"].(string)
			_, isRelationship := value["from"]
			if isEntityID(id) && !isRelationship {
				ref := EntityRef{ID: id}
				ref.Name, _ = value["name"].(string)
				ref.Kind, _ = value["kind"].(string)
				if source, ok := value["source"].(map[string]any); ok {
					ref.Source.File, _ = source["file"].(string)
					if line, ok := source["line"].(float64); ok {
						ref.Source.Line = int(line)
					}
					if line, ok := source["endLine"].(float64); ok {
						ref.Source.EndLine = int(line)
					}
				}
				if files, ok := value["files"].([]any); ok {
					for _, file := range files {
						if file, ok := file.(string); ok {
							ref.Files = append(ref.Files, file)
						}
					}
				}
				if !seen[id] {
					seen[id] = true
					refs = append(refs, ref)
				}
			}
			for _, item := range value {
				walk(item)
			}
		}
	}
	for _, value := range embeddedJSONValues(data) {
		walk(value)
	}

	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID == refs[j].ID {
			return refs[i].Source.Line < refs[j].Source.Line
		}
		return refs[i].ID < refs[j].ID
	})
	return refs
}

func isEntityID(id string) bool {
	for _, kind := range []string{"controller", "function", "crd", "package", "test", "document", "resource", "template", "operator"} {
		if strings.HasPrefix(id, kind+":") {
			return true
		}
	}
	return false
}

// RelationshipRefs extracts relationship objects from a JSON query result.
// Only objects with both endpoints and a type are accepted, which prevents an
// entity's ordinary id field from being mistaken for an edge.
func RelationshipRefs(data string) []RelationshipRef {
	seen := make(map[string]bool)
	var refs []RelationshipRef
	var walk func(any)
	walk = func(current any) {
		switch value := current.(type) {
		case []any:
			for _, item := range value {
				walk(item)
			}
		case map[string]any:
			from, fromOK := value["from"].(string)
			to, toOK := value["to"].(string)
			typeName, typeOK := value["type"].(string)
			id, _ := value["id"].(string)
			if fromOK && toOK && typeOK && id != "" {
				ref := RelationshipRef{ID: id, From: from, To: to, Type: typeName}
				ref.Confidence, _ = value["confidence"].(string)
				if evidence, ok := value["evidence"].(map[string]any); ok {
					ref.Evidence.Parser, _ = evidence["parser"].(string)
					ref.Evidence.File, _ = evidence["file"].(string)
					if line, ok := evidence["line"].(float64); ok {
						ref.Evidence.Line = int(line)
					}
					ref.Evidence.Reason, _ = evidence["reason"].(string)
					ref.Evidence.Snippet, _ = evidence["snippet"].(string)
				}
				if !seen[id] {
					seen[id] = true
					refs = append(refs, ref)
				}
			}
			for _, item := range value {
				walk(item)
			}
		}
	}
	for _, value := range embeddedJSONValues(data) {
		walk(value)
	}

	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs
}

// embeddedJSONValues extracts complete JSON objects/arrays from a mixed Atlas
// payload. Assistant workflows intentionally concatenate several bounded JSON
// query results with human-readable section headings; requiring the entire
// payload to be one JSON document would silently discard the structured graph
// evidence in every section after the heading.
func embeddedJSONValues(data string) []any {
	var values []any
	for offset := 0; offset < len(data); {
		relative := strings.IndexAny(data[offset:], "{[")
		if relative < 0 {
			break
		}
		start := offset + relative
		decoder := json.NewDecoder(strings.NewReader(data[start:]))
		var value any
		if err := decoder.Decode(&value); err != nil {
			offset = start + 1
			continue
		}
		values = append(values, value)
		consumed := int(decoder.InputOffset())
		if consumed <= 0 {
			offset = start + 1
			continue
		}
		offset = start + consumed
	}
	return values
}

// IsAmbiguous reports whether a structured Ask result explicitly requires an
// exact entity ID. Consumers should stop before invoking an LLM in this case.
func IsAmbiguous(data string) bool {
	var value struct {
		Ambiguous bool `json:"ambiguous"`
	}
	return json.Unmarshal([]byte(data), &value) == nil && value.Ambiguous
}
