package prolog

import (
	"context"
	"testing"
)

// runProg parses src (honouring :- dynamic), runs goal, returns all bindings of
// wantVar and the machine (for inspecting mutations / a second run).
func runProg(t *testing.T, src, goal, wantVar string, max int) ([]string, *Machine) {
	t.Helper()
	prog := Parse(src)
	for _, d := range prog.Diagnostics {
		if d.Kind == DiagSyntax {
			t.Fatalf("program diagnostic: %v", d)
		}
	}
	m := NewMachineFromProgram(prog)
	g, diags, err := ParseTerm(goal)
	if err != nil {
		t.Fatalf("parse goal %q: %v (%v)", goal, err, diags)
	}
	sols, err := m.Solve(context.Background(), []Term{g}, max)
	if err != nil {
		t.Fatalf("solve %q: %v", goal, err)
	}
	out := make([]string, len(sols))
	for i, s := range sols {
		if v := s[wantVar]; v != nil {
			out[i] = v.String()
		}
	}
	return out, m
}

func TestDynamicDirectiveParsed(t *testing.T) {
	// Both prefix and functor forms of the directive.
	if got := Parse(":- dynamic counter/1.").Dynamic; len(got) != 1 || got[0] != "counter/1" {
		t.Errorf("prefix dynamic => %v, want [counter/1]", got)
	}
	if got := Parse(":- dynamic(foo/2).").Dynamic; len(got) != 1 || got[0] != "foo/2" {
		t.Errorf("functor dynamic => %v, want [foo/2]", got)
	}
	if got := Parse(":- dynamic foo/2, bar/0.").Dynamic; len(got) != 2 {
		t.Errorf("comma-list dynamic => %v, want 2 entries", got)
	}
}

func TestAssertAndQuery(t *testing.T) {
	src := `:- dynamic fact/1.`
	got, _ := runProg(t, src, "assertz(fact(a)), assertz(fact(b)), fact(X)", "X", 0)
	if !eq(got, []string{"a", "b"}) {
		t.Errorf("assertz then query => %v, want [a b]", got)
	}
}

func TestAsertaOrder(t *testing.T) {
	src := `:- dynamic fact/1.`
	got, _ := runProg(t, src, "assertz(fact(a)), asserta(fact(z)), fact(X)", "X", 0)
	if !eq(got, []string{"z", "a"}) {
		t.Errorf("asserta prepends => %v, want [z a]", got)
	}
}

func TestRetract(t *testing.T) {
	src := `
		:- dynamic fact/1.
		fact(a).
		fact(b).
		fact(c).
	`
	got, _ := runProg(t, src, "retract(fact(b)), fact(X)", "X", 0)
	if !eq(got, []string{"a", "c"}) {
		t.Errorf("after retract(b) => %v, want [a c]", got)
	}
}

func TestMutationsAreRunScoped(t *testing.T) {
	src := `:- dynamic fact/1.`
	m := NewMachineFromProgram(Parse(src))

	// First run asserts a fact.
	g1, _, _ := ParseTerm("assertz(fact(x)), fact(X)")
	sols, err := m.Solve(context.Background(), []Term{g1}, 0)
	if err != nil || len(sols) != 1 {
		t.Fatalf("first run: sols=%v err=%v", sols, err)
	}
	// Second run must NOT see the fact from the first run.
	g2, _, _ := ParseTerm("fact(X)")
	sols2, err := m.Solve(context.Background(), []Term{g2}, 0)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(sols2) != 0 {
		t.Errorf("mutation leaked across runs: got %d solutions", len(sols2))
	}
}

func TestAssertOnStaticThrows(t *testing.T) {
	// fact/1 is NOT declared dynamic → assert throws permission_error.
	prog := Parse(`fact(a).`)
	m := NewMachineFromProgram(prog)
	g, _, _ := ParseTerm("catch(assertz(fact(b)), error(E, _), true)")
	sols, err := m.Solve(context.Background(), []Term{g}, 1)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if len(sols) != 1 || sols[0]["E"].String() != "permission_error(modify,static_procedure,fact/1)" {
		t.Errorf("static assert => %v, want permission_error", sols)
	}
}

func TestMutationsAudit(t *testing.T) {
	src := `:- dynamic fact/1.`
	_, m := runProg(t, src, "assertz(fact(a)), asserta(fact(z))", "_", 1)
	muts := m.Mutations()
	if len(muts) != 2 || muts[0].Op != "assertz" || muts[1].Op != "asserta" {
		t.Errorf("mutation audit => %+v, want [assertz asserta]", muts)
	}
}

// A counter loop: assert/retract used to carry state across backtracking within
// one run, exercising the logical-update view.
func TestCounterPattern(t *testing.T) {
	src := `
		:- dynamic count/1.
		count(0).
		bump :- retract(count(N)), N1 is N + 1, assertz(count(N1)).
	`
	got, _ := runProg(t, src, "bump, bump, bump, count(X)", "X", 0)
	if !eq(got, []string{"3"}) {
		t.Errorf("counter => %v, want [3]", got)
	}
}
