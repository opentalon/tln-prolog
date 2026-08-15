package prolog_test

import (
	"context"
	"fmt"
	"testing"

	fs "github.com/opentalon/tln-language/pkg/factstore"
	prolog "github.com/opentalon/tln-prolog"
)

// ExampleMachine_Solve runs a recursive program and prints the answers — the
// engine end to end: reader → SLD resolution → solutions.
func ExampleMachine_Solve() {
	prog := prolog.Parse(`
		parent(tom, bob).
		parent(bob, ann).
		ancestor(X, Y) :- parent(X, Y).
		ancestor(X, Y) :- parent(X, Z), ancestor(Z, Y).
	`)
	goal, _, _ := prolog.ParseTerm("ancestor(tom, D)")
	sols, _ := prolog.NewMachine(prog.Clauses).Solve(context.Background(), []prolog.Term{goal}, 0)
	fmt.Println(len(sols), "descendants")
	// Output: 2 descendants
}

// TestSolutionsFeedBackAsFacts is the boundary in action: solve a recursive
// reachability query, project each answer to a unary EAV fact, and assert them
// into a tln factstore.MemoryStore — Prolog answers flowing back into the store
// a host queries. This is the runtime backstop the porting pipeline builds on.
//
// Integer node ids are used because the in-process MemoryStore keys entities by
// integer id; the AtomFacts boundary is store-agnostic, so a document backend
// like tln-db takes string ids just the same.
func TestSolutionsFeedBackAsFacts(t *testing.T) {
	prog := prolog.Parse(`
		adjacent(1, 2).
		adjacent(2, 3).
		reaches(X, Y) :- adjacent(X, Y).
		reaches(X, Y) :- adjacent(X, Z), reaches(Z, Y).
	`)
	tmpl, _, err := prolog.ParseTerm("reaches(1, D)")
	if err != nil {
		t.Fatalf("parse goal: %v", err)
	}
	m := prolog.NewMachine(prog.Clauses)
	sols, err := m.Solve(context.Background(), []prolog.Term{tmpl}, 0)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if len(sols) != 2 {
		t.Fatalf("want reaches {2,3}, got %d solutions", len(sols))
	}

	// Project each answer as the unary atom reachable(D) so it lands as an
	// entity keyed by the integer node id.
	atoms := make([]prolog.Term, len(sols))
	for i, s := range sols {
		atoms[i] = prolog.Compound{Functor: "reachable", Args: []prolog.Term{s["D"]}}
	}
	facts, diags := prolog.AtomFacts(atoms)
	if len(diags) != 0 {
		t.Fatalf("unexpected projection diagnostics: %v", diags)
	}

	store := fs.NewMemoryStore()
	if err := store.Assert(context.Background(), facts); err != nil {
		t.Fatalf("assert: %v", err)
	}

	rows, err := store.Query(context.Background(), fs.Query{
		Find:  []string{"?e"},
		Where: []fs.Clause{&fs.Pattern{Entity: fs.Var("e"), Attribute: prolog.Namespace + "reachable", Value: fs.Lit(true)}},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 reachable facts in store, got %d (%v)", len(rows), rows)
	}
}

// TestAtomFactsEncoding pins the nullary/unary/n-ary projection shapes.
func TestAtomFactsEncoding(t *testing.T) {
	nullary, _, _ := prolog.ParseTerm("winter")
	unary, _, _ := prolog.ParseTerm("winning(pos1)")
	nary, _, _ := prolog.ParseTerm("edge(a, b)")

	facts, _ := prolog.AtomFacts([]prolog.Term{nullary, unary, nary})
	if len(facts) != 3 {
		t.Fatalf("want 3 facts, got %d", len(facts))
	}
	if facts[0].Attribute != prolog.Namespace+"holds" || facts[0].RecordID != "winter" {
		t.Errorf("nullary encoding wrong: %+v", facts[0])
	}
	if facts[1].Attribute != prolog.Namespace+"winning" || facts[1].RecordID != "pos1" || facts[1].Value != true {
		t.Errorf("unary encoding wrong: %+v", facts[1])
	}
	if facts[2].Attribute != prolog.Namespace+"edge" {
		t.Errorf("n-ary encoding wrong: %+v", facts[2])
	}
	tuple, ok := facts[2].Value.([]any)
	if !ok || len(tuple) != 2 || tuple[0] != "a" || tuple[1] != "b" {
		t.Errorf("n-ary tuple value wrong: %+v", facts[2].Value)
	}
}
