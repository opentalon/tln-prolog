package prolog_test

import (
	"context"
	"testing"

	"github.com/opentalon/tln-language/pkg/tln"
	prolog "github.com/opentalon/tln-prolog"
)

func TestFactory_SatisfiesPluginFactory(t *testing.T) {
	var _ tln.PluginFactory = prolog.Factory
}

// TestFactory_QueryReturnsSolutions runs a recursive program end to end through
// the ToolResolver adapter and checks every solution comes back as a fact.
func TestFactory_QueryReturnsSolutions(t *testing.T) {
	r, err := prolog.Factory(tln.ConnectorSpec{Name: "reason", Plugin: "prolog"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	res, err := r.Call(context.Background(), "reason", "query", map[string]any{
		"program": `
			parent(tom, bob).
			parent(bob, ann).
			ancestor(X, Y) :- parent(X, Y).
			ancestor(X, Y) :- parent(X, Z), ancestor(Z, Y).`,
		"goal": "ancestor(tom, D)",
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	rows, ok := res.([]map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want []map[string]any", res)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 solutions (bob, ann), got %d: %#v", len(rows), rows)
	}
	for _, row := range rows {
		if row["attribute"] != ":pl/ancestor" {
			t.Errorf("unexpected attribute in %v", row)
		}
	}
}

func TestFactory_MissingGoalErrors(t *testing.T) {
	r, _ := prolog.Factory(tln.ConnectorSpec{Name: "reason"})
	if _, err := r.Call(context.Background(), "reason", "query", map[string]any{"program": "p(a)."}); err == nil {
		t.Fatal("expected error when goal is missing")
	}
}

func TestFactory_UnknownTool(t *testing.T) {
	r, _ := prolog.Factory(tln.ConnectorSpec{Name: "reason"})
	if _, err := r.Call(context.Background(), "reason", "solve", nil); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
