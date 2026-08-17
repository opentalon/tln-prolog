package prolog

import (
	"context"
	"fmt"

	"github.com/opentalon/tln-language/pkg/tln"
)

// Factory builds a ToolResolver that runs Prolog queries as tool calls, so
// tln-prolog can be loaded by name via mod.tln + a connector (ADR 0012/0013):
//
//	connector "reason" via prolog { }
//	tool "reason" "query" {
//	  program "parent(tom,bob). ancestor(X,Y):-parent(X,Y)."
//	  goal    "ancestor(tom, D)"
//	}
//
// The "query" tool parses `program` (.pl source), proves `goal`, and returns
// one row per solution — each the ground goal projected to a fact
// (record_id / attribute / value), the same shape [AtomFacts] produces.
func Factory(spec tln.ConnectorSpec) (tln.ToolResolver, error) {
	return resolver{}, nil
}

// Factory satisfies tln.PluginFactory.
var _ tln.PluginFactory = Factory

type resolver struct{}

func (resolver) Call(ctx context.Context, server, tool string, args map[string]any) (any, error) {
	if tool != "query" {
		return nil, fmt.Errorf("tln-prolog: unknown tool %q on server %q (want \"query\")", tool, server)
	}
	programSrc, _ := args["program"].(string)
	goalSrc, _ := args["goal"].(string)
	if goalSrc == "" {
		return nil, fmt.Errorf("tln-prolog: \"query\" requires a \"goal\" argument")
	}
	goal, _, err := ParseTerm(goalSrc)
	if err != nil {
		return nil, fmt.Errorf("tln-prolog: parse goal %q: %w", goalSrc, err)
	}
	m := NewMachineFromProgram(Parse(programSrc))
	sols, err := m.Solve(ctx, []Term{goal}, 0)
	if err != nil {
		return nil, err
	}
	atoms := make([]Term, len(sols))
	for i, s := range sols {
		atoms[i] = Instantiate(goal, s)
	}
	facts, _ := AtomFacts(atoms)
	out := make([]map[string]any, len(facts))
	for i, f := range facts {
		out[i] = map[string]any{"record_id": f.RecordID, "attribute": f.Attribute, "value": f.Value}
	}
	return out, nil
}
