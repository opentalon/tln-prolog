package prolog

import (
	"context"
	"testing"
)

// solveN parses program + goal and returns all solutions (up to max, 0=all) as
// the bound value of wantVar for each.
func solveN(t *testing.T, program, goal, wantVar string, max int) []string {
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
	sols, err := NewMachine(prog.Clauses).Solve(context.Background(), []Term{g}, max)
	if err != nil {
		t.Fatalf("solve %q: %v", goal, err)
	}
	out := make([]string, len(sols))
	for i, s := range sols {
		out[i] = s[wantVar].String()
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCutPrunesClauseAlternatives(t *testing.T) {
	prog := `
		p(1). p(2). p(3).
		first(X) :- p(X), !.
	`
	if got := solveN(t, prog, "first(X)", "X", 0); !eq(got, []string{"1"}) {
		t.Errorf("first(X) => %v, want [1]", got)
	}
}

// A cut in the middle of a body must prune choice points of goals *before* it,
// not only the predicate's own remaining clauses.
func TestCutPrunesEarlierChoicePoints(t *testing.T) {
	prog := `
		a(1). a(2).
		b(1). b(2).
		q(X, Y) :- a(X), !, b(Y).
	`
	// a(X) commits to X=1; b(Y) still backtracks over 1,2. X=2 must never appear.
	xs := solveN(t, prog, "q(X, Y)", "X", 0)
	if !eq(xs, []string{"1", "1"}) {
		t.Errorf("q/2 X bindings => %v, want [1 1]", xs)
	}
	ys := solveN(t, prog, "q(X, Y)", "Y", 0)
	if !eq(ys, []string{"1", "2"}) {
		t.Errorf("q/2 Y bindings => %v, want [1 2]", ys)
	}
}

func TestNegationAsFailure(t *testing.T) {
	prog := `p(1). p(2).`
	proveTrue(t, prog, "\\+ p(3)")
	proveFalse(t, prog, "\\+ p(1)")
	// not/1 functor form is the alias.
	proveTrue(t, prog, "not(p(3))")
	proveFalse(t, prog, "not(p(2))")
}

func TestOnce(t *testing.T) {
	prog := `p(1). p(2). p(3).`
	if got := solveN(t, prog, "once(p(X))", "X", 0); !eq(got, []string{"1"}) {
		t.Errorf("once(p(X)) => %v, want [1]", got)
	}
}

func TestIfThenElse(t *testing.T) {
	// Condition true -> Then branch.
	if got := solveN(t, "", "( 5 > 3 -> R = yes ; R = no )", "R", 0); !eq(got, []string{"yes"}) {
		t.Errorf("ite true => %v, want [yes]", got)
	}
	// Condition false -> Else branch.
	if got := solveN(t, "", "( 2 > 3 -> R = yes ; R = no )", "R", 0); !eq(got, []string{"no"}) {
		t.Errorf("ite false => %v, want [no]", got)
	}
	// Bare if-then with a false condition fails (no solution, no else).
	if got := solveN(t, "", "( 2 > 3 -> R = yes )", "R", 0); len(got) != 0 {
		t.Errorf("bare if-then false => %v, want []", got)
	}
}

func TestDisjunction(t *testing.T) {
	if got := solveN(t, "", "( X = a ; X = b )", "X", 0); !eq(got, []string{"a", "b"}) {
		t.Errorf("disjunction => %v, want [a b]", got)
	}
}

func proveTrue(t *testing.T, program, goal string) {
	t.Helper()
	if !prove(t, program, goal) {
		t.Errorf("%s should hold", goal)
	}
}

func proveFalse(t *testing.T, program, goal string) {
	t.Helper()
	if prove(t, program, goal) {
		t.Errorf("%s should not hold", goal)
	}
}

func prove(t *testing.T, program, goal string) bool {
	t.Helper()
	prog := Parse(program)
	g, diags, err := ParseTerm(goal)
	if err != nil {
		t.Fatalf("parse goal %q: %v (%v)", goal, err, diags)
	}
	ok, err := NewMachine(prog.Clauses).Prove(context.Background(), []Term{g})
	if err != nil {
		t.Fatalf("prove %q: %v", goal, err)
	}
	return ok
}
