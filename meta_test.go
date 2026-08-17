package prolog

import (
	"context"
	"testing"
)

// solveList parses program + goal and returns the first solution's binding for
// wantVar rendered as a string (canonical term notation).
func solveList(t *testing.T, program, goal, wantVar string) string {
	t.Helper()
	prog := Parse(program)
	for _, d := range prog.Diagnostics {
		if d.Kind == DiagSyntax {
			t.Fatalf("program diagnostic: %v", d)
		}
	}
	g, diags, err := ParseTerm(goal)
	if err != nil {
		t.Fatalf("parse goal %q: %v (%v)", goal, err, diags)
	}
	sols, err := NewMachine(prog.Clauses).Solve(context.Background(), []Term{g}, 1)
	if err != nil {
		t.Fatalf("solve %q: %v", goal, err)
	}
	if len(sols) == 0 {
		return "<none>"
	}
	return sols[0][wantVar].String()
}

func TestFindall(t *testing.T) {
	prog := `p(1). p(2). p(3).`
	if got := solveList(t, prog, "findall(X, p(X), L)", "L"); got != "[1,2,3]" {
		t.Errorf("findall => %q, want [1,2,3]", got)
	}
	// findall yields [] (does not fail) when the goal has no solutions.
	if got := solveList(t, prog, "findall(X, p(X), L), L = []", "L"); got != "<none>" {
		// p/1 has solutions, so L=[] should fail — sanity that findall isn't empty here
		t.Errorf("expected non-empty findall to not unify with []")
	}
	if got := solveList(t, "", "findall(X, fail, L)", "L"); got != "[]" {
		t.Errorf("findall over fail => %q, want []", got)
	}
}

func TestCopyTerm(t *testing.T) {
	// copy_term produces a structurally equal term with fresh variables, so it
	// unifies with a fresh pattern but shares no bindings with the original.
	prog := ``
	if got := solveList(t, prog, "copy_term(f(X, X, a), C), C = f(P, Q, R)", "R"); got != "a" {
		t.Errorf("copy_term ground arg => %q, want a", got)
	}
	// Shared variable stays shared in the copy: f(Y,Y) copied then one side
	// bound propagates to the other.
	if got := solveList(t, prog, "copy_term(f(Y, Y), f(1, Z))", "Z"); got != "1" {
		t.Errorf("copy_term shared var => %q, want 1", got)
	}
}

func TestStandardOrderAndCompare(t *testing.T) {
	proveTrue(t, "", "1 @< a")       // number < atom
	proveTrue(t, "", "a @< f(x)")    // atom < compound
	proveTrue(t, "", "f(1) @< f(2)") // by argument
	proveTrue(t, "", "a == a")
	proveTrue(t, "", "a \\== b")
	if got := solveList(t, "", "compare(O, 1, 2)", "O"); got != "<" {
		t.Errorf("compare(O,1,2) => %q, want <", got)
	}
	if got := solveList(t, "", "compare(O, foo, foo)", "O"); got != "=" {
		t.Errorf("compare(O,foo,foo) => %q, want =", got)
	}
}

func TestSetofSortsAndDedups(t *testing.T) {
	prog := `n(3). n(1). n(2). n(1).`
	if got := solveList(t, prog, "setof(X, n(X), L)", "L"); got != "[1,2,3]" {
		t.Errorf("setof => %q, want [1,2,3] (sorted, deduped)", got)
	}
	// bagof keeps order and duplicates.
	if got := solveList(t, prog, "bagof(X, n(X), L)", "L"); got != "[3,1,2,1]" {
		t.Errorf("bagof => %q, want [3,1,2,1]", got)
	}
	// setof fails when the goal has no solutions.
	if got := solveList(t, prog, "setof(X, fail, L)", "L"); got != "<none>" {
		t.Errorf("setof over fail => %q, want <none>", got)
	}
}

func TestSetofFreeVariableGrouping(t *testing.T) {
	// age/2 with a free variable Name: setof(A, age(Name,A), L) backtracks over
	// each Name, collecting that person's ages.
	prog := `
		age(ann, 5).
		age(bob, 7).
		age(ann, 6).
	`
	// For ann: [5,6]; for bob: [7]. Two solutions, ordered by Name (standard order).
	m := NewMachine(Parse(prog).Clauses)
	g, _, err := ParseTerm("setof(A, age(N, A), L)")
	if err != nil {
		t.Fatal(err)
	}
	sols, err := m.Solve(context.Background(), []Term{g}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sols) != 2 {
		t.Fatalf("want 2 groups, got %d: %v", len(sols), sols)
	}
	if sols[0]["N"].String() != "ann" || sols[0]["L"].String() != "[5,6]" {
		t.Errorf("group 0 => N=%s L=%s, want ann [5,6]", sols[0]["N"], sols[0]["L"])
	}
	if sols[1]["N"].String() != "bob" || sols[1]["L"].String() != "[7]" {
		t.Errorf("group 1 => N=%s L=%s, want bob [7]", sols[1]["N"], sols[1]["L"])
	}
}

func TestSetofExistentialCaret(t *testing.T) {
	// N^age(N,A) quantifies N away, so setof collects ALL ages across names into
	// one sorted, deduped list rather than grouping by N.
	prog := `
		age(ann, 5).
		age(bob, 7).
		age(ann, 6).
	`
	if got := solveList(t, prog, "setof(A, N^age(N, A), L)", "L"); got != "[5,6,7]" {
		t.Errorf("setof with ^ => %q, want [5,6,7]", got)
	}
}
