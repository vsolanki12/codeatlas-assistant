package claude

import (
	"fmt"
	"strings"
	"testing"
)

type noCallAtlas struct {
	calls []string
}

func (r *noCallAtlas) Run(args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	return "", fmt.Errorf("unexpected Atlas call")
}

func (r *noCallAtlas) RunJSON(args ...string) (string, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	return "", fmt.Errorf("unexpected Atlas call")
}

func (r *noCallAtlas) GraphPath() string { return "" }

func TestGatherControllerDataReusesExistingEntityContext(t *testing.T) {
	controllerID := "controller:example.com/repo/pkg.Reconciler"
	existing := fmt.Sprintf(`{"entities":[{"id":%q,"kind":"controller","source":{"file":"controllers/reconcile.go"}}],"relationships":[]}`, controllerID)
	runner := &noCallAtlas{}
	if got := gatherControllerData(runner, existing, controllerID); got != existing {
		t.Fatalf("existing Atlas context was not reused: %q", got)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("controller context was redundantly queried: %v", runner.calls)
	}
}
