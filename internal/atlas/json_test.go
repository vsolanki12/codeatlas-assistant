package atlas

import "testing"

func TestEntityRefsDoesNotTreatRelationshipsAsEntities(t *testing.T) {
	data := `{"entity":{"id":"controller:pkg.Controller","name":"Controller","kind":"controller"},"relationships":[{"id":"controller:pkg.Controller--creates--resource:deployment.ns/app","from":"controller:pkg.Controller","to":"resource:deployment.ns/app","type":"creates","confidence":"inferred","evidence":{"file":"controller.go","line":10}}]}`
	refs := EntityRefs(data)
	if len(refs) != 1 || refs[0].ID != "controller:pkg.Controller" {
		t.Fatalf("EntityRefs() = %+v, want only entity object", refs)
	}
	rels := RelationshipRefs(data)
	if len(rels) != 1 || rels[0].Type != "creates" || rels[0].Evidence.Line != 10 {
		t.Fatalf("RelationshipRefs() = %+v, want evidence-bearing relationship", rels)
	}
}

func TestEntityAndRelationshipRefsReadMixedAtlasSections(t *testing.T) {
	data := `### Search: Widget
{"entities":[{"id":"crd:example.io.Widget","name":"Widget","kind":"crd","source":{"file":"config/widget.yaml","line":1}}]}
### Ask (debug): Widget
{"entity":{"id":"controller:pkg.WidgetReconciler","name":"WidgetReconciler","kind":"controller","source":{"file":"controllers/widget.go","line":22}},"relationships":[{"id":"controller:pkg.WidgetReconciler--reconciles--crd:example.io.Widget","from":"controller:pkg.WidgetReconciler","to":"crd:example.io.Widget","type":"reconciles","confidence":"proven","evidence":{"parser":"go-ast","file":"controllers/widget.go","line":30,"reason":"For registration"}}]}`

	entities := EntityRefs(data)
	if len(entities) != 2 || entities[0].ID != "controller:pkg.WidgetReconciler" || entities[1].ID != "crd:example.io.Widget" {
		t.Fatalf("EntityRefs() = %+v, want entities from both JSON sections", entities)
	}
	rels := RelationshipRefs(data)
	if len(rels) != 1 || rels[0].From != "controller:pkg.WidgetReconciler" || rels[0].Evidence.File != "controllers/widget.go" {
		t.Fatalf("RelationshipRefs() = %+v, want relationship from mixed JSON sections", rels)
	}
}

func TestIsAmbiguous(t *testing.T) {
	if !IsAmbiguous(`{"ambiguous":true,"candidates":[]}`) {
		t.Fatal("expected explicit ambiguity")
	}
	if IsAmbiguous(`{"ambiguous":false}`) {
		t.Fatal("did not expect ambiguity")
	}
}

func TestEntityRefsAcceptTemplateEntities(t *testing.T) {
	data := `{"entities":[{"id":"template:kubernetes.deployment@config/deployment.yaml#1","name":"Deployment template","kind":"template","source":{"file":"config/deployment.yaml","line":1}}]}`
	refs := EntityRefs(data)
	if len(refs) != 1 || refs[0].ID != "template:kubernetes.deployment@config/deployment.yaml#1" {
		t.Fatalf("EntityRefs() = %+v, want template entity", refs)
	}
}

func TestClientUsageAccumulatesDeterministically(t *testing.T) {
	c := &Client{}
	c.recordUsage("search", "abcd")
	c.recordUsage("ask", "éé")
	c.recordUsage("ask", "z")

	usage := c.LastUsage()
	if usage.Calls != 3 || usage.Response.Bytes != 9 || usage.Response.Characters != 7 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
	if usage.Commands["ask"] != 2 || usage.Commands["search"] != 1 {
		t.Fatalf("unexpected command counts: %+v", usage.Commands)
	}
	if got := usage.Summary(); got != "3 calls, 9 bytes, 7 chars, ~3 estimated tokens response, commands: ask=2, search=1" {
		t.Fatalf("unexpected summary: %q", got)
	}
}
