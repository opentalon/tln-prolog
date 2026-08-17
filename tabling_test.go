package prolog

import (
	"context"
	"testing"
)

// wfs parses src, runs a throwaway query to build the model, and returns it.
func wfs(t *testing.T, src, probe string) (WFSResult, *Machine) {
	t.Helper()
	prog := Parse(src)
	for _, d := range prog.Diagnostics {
		if d.Kind == DiagSyntax {
			t.Fatalf("program diagnostic: %v", d)
		}
	}
	m := NewMachineFromProgram(prog)
	g, diags, err := ParseTerm(probe)
	if err != nil {
		t.Fatalf("parse probe %q: %v (%v)", probe, err, diags)
	}
	if _, err := m.Solve(context.Background(), []Term{g}, 0); err != nil {
		t.Fatalf("solve: %v", err)
	}
	return m.WellFounded(), m
}

func TestTableDirectiveParsed(t *testing.T) {
	if got := Parse(":- table win/1.").Tabled; len(got) != 1 || got[0] != "win/1" {
		t.Errorf("table directive => %v, want [win/1]", got)
	}
}

// Left recursion that would hit ErrDepthExceeded under plain SLD terminates and
// is complete under tabling.
func TestTabledReachabilityTerminates(t *testing.T) {
	src := `
		:- table reach/2.
		edge(a, b).
		edge(b, c).
		edge(c, a).
		reach(X, Y) :- edge(X, Y).
		reach(X, Y) :- edge(X, Z), reach(Z, Y).
	`
	// From a, every node is reachable (cycle a->b->c->a).
	got, _ := runProgTabled(t, src, "reach(a, Y)", "Y")
	if !eq(got, []string{"a", "b", "c"}) {
		t.Errorf("reach(a,Y) => %v, want [a b c]", got)
	}
}

// The classic win/lose game exercises non-stratified negation. WFS assigns each
// position true (win), false (loss), or undefined (draw).
func TestWinLoseGame(t *testing.T) {
	// a -> b -> c (terminal). c is a loss, b a win, a a loss.
	src := `
		:- table win/1.
		move(a, b).
		move(b, c).
		win(X) :- move(X, Y), \+ win(Y).
	`
	model, m := wfs(t, src, "win(_)")
	if !eq(model.True, []string{"win(b)"}) {
		t.Errorf("true => %v, want [win(b)]", model.True)
	}
	if !eq(model.False, []string{"win(a)", "win(c)"}) {
		t.Errorf("false => %v, want [win(a) win(c)]", model.False)
	}
	if len(model.Undefined) != 0 {
		t.Errorf("undefined => %v, want []", model.Undefined)
	}
	// \+ win(c): c is a loss (false) → holds. \+ win(b): b is a win → fails.
	if ok, _ := m.Prove(context.Background(), []Term{mustGoal(t, "\\+ win(c)")}); !ok {
		t.Error("\\+ win(c) should hold (c is a loss)")
	}
	if ok, _ := m.Prove(context.Background(), []Term{mustGoal(t, "\\+ win(b)")}); ok {
		t.Error("\\+ win(b) should fail (b is a win)")
	}
}

// A 2-cycle is a drawn game: both positions are undefined in the well-founded
// model, and \+ win holds for neither.
func TestWinLoseDrawCycle(t *testing.T) {
	src := `
		:- table win/1.
		move(a, b).
		move(b, a).
		win(X) :- move(X, Y), \+ win(Y).
	`
	model, m := wfs(t, src, "win(_)")
	if len(model.True) != 0 || len(model.False) != 0 {
		t.Errorf("draw: true=%v false=%v, want both empty", model.True, model.False)
	}
	if !eq(model.Undefined, []string{"win(a)", "win(b)"}) {
		t.Errorf("undefined => %v, want [win(a) win(b)]", model.Undefined)
	}
	// win(a) is undefined: neither win(a) nor \+ win(a) succeeds.
	if ok, _ := m.Prove(context.Background(), []Term{mustGoal(t, "win(a)")}); ok {
		t.Error("win(a) should not succeed (undefined)")
	}
	if ok, _ := m.Prove(context.Background(), []Term{mustGoal(t, "\\+ win(a)")}); ok {
		t.Error("\\+ win(a) should not succeed (undefined, not false)")
	}
}

// Stratified negation still gives the expected two-valued answer.
func TestStratifiedNegation(t *testing.T) {
	src := `
		:- table p/1.
		:- table q/1.
		base(1). base(2). base(3).
		q(2).
		p(X) :- base(X), \+ q(X).
	`
	got, _ := runProgTabled(t, src, "p(X)", "X")
	if !eq(got, []string{"1", "3"}) {
		t.Errorf("p(X) => %v, want [1 3] (2 excluded by \\+ q(2))", got)
	}
}

// runProgTabled solves a tabled query and returns sorted bindings of wantVar.
func runProgTabled(t *testing.T, src, goal, wantVar string) ([]string, *Machine) {
	t.Helper()
	prog := Parse(src)
	m := NewMachineFromProgram(prog)
	g, diags, err := ParseTerm(goal)
	if err != nil {
		t.Fatalf("parse goal %q: %v (%v)", goal, err, diags)
	}
	sols, err := m.Solve(context.Background(), []Term{g}, 0)
	if err != nil {
		t.Fatalf("solve %q: %v", goal, err)
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range sols {
		if v := s[wantVar]; v != nil {
			seen[v.String()] = true
		}
	}
	for k := range seen {
		out = append(out, k)
	}
	sortStrings(out)
	return out, m
}

func mustGoal(t *testing.T, src string) Term {
	t.Helper()
	g, _, err := ParseTerm(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return g
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
