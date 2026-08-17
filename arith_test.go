package prolog

import (
	"context"
	"testing"
)

// solveArith parses a program, runs the goal, and returns the first solution's
// binding for the named variable (as a term string), or "" if no solution.
func solveArith(t *testing.T, program, goal, wantVar string) string {
	t.Helper()
	prog := Parse(program)
	for _, d := range prog.Diagnostics {
		if d.Kind == DiagSyntax {
			t.Fatalf("unexpected diagnostic: %v", d)
		}
	}
	g, diags, err := ParseTerm(goal)
	if err != nil {
		t.Fatalf("parse goal %q: %v (%v)", goal, err, diags)
	}
	m := NewMachine(prog.Clauses)
	sols, err := m.Solve(context.Background(), []Term{g}, 1)
	if err != nil {
		t.Fatalf("solve %q: %v", goal, err)
	}
	if len(sols) == 0 {
		return ""
	}
	return sols[0][wantVar].String()
}

func TestIsEvaluatesArithmetic(t *testing.T) {
	cases := []struct {
		goal string
		want string
	}{
		{"X is 2 + 3 * 4", "14"},   // precedence: * binds tighter
		{"X is (2 + 3) * 4", "20"}, // parenthesised
		{"X is 7 // 2", "3"},       // integer division
		{"X is 7 mod 3", "1"},      // modulo
		{"X is 2 + 0.5", "2.5"},    // int + float -> float
		{"X is 10 / 4", "2.5"},     // "/" is float division
		{"X is -3 + 1", "-2"},      // prefix minus literal
		{"X is abs(-5)", "5"},      // unary function
	}
	for _, c := range cases {
		if got := solveArith(t, "", c.goal, "X"); got != c.want {
			t.Errorf("%s => %q, want %q", c.goal, got, c.want)
		}
	}
}

func TestArithComparisons(t *testing.T) {
	yes := []string{"2 < 3", "3 =< 3", "5 > 2", "3 >= 3", "4 =:= 2 + 2", "4 =\\= 5"}
	for _, g := range yes {
		prog := Parse("")
		gt, _, err := ParseTerm(g)
		if err != nil {
			t.Fatalf("parse %q: %v", g, err)
		}
		ok, err := NewMachine(prog.Clauses).Prove(context.Background(), []Term{gt})
		if err != nil {
			t.Fatalf("prove %q: %v", g, err)
		}
		if !ok {
			t.Errorf("%s should hold", g)
		}
	}
	no := []string{"3 < 2", "4 =:= 5", "3 =\\= 3"}
	for _, g := range no {
		prog := Parse("")
		gt, _, err := ParseTerm(g)
		if err != nil {
			t.Fatalf("parse %q: %v", g, err)
		}
		ok, err := NewMachine(prog.Clauses).Prove(context.Background(), []Term{gt})
		if err != nil {
			t.Fatalf("prove %q: %v", g, err)
		}
		if ok {
			t.Errorf("%s should not hold", g)
		}
	}
}

func TestIsWithRecursion(t *testing.T) {
	// A classic: length of a list via arithmetic accumulation.
	prog := `
		len([], 0).
		len([_|T], N) :- len(T, N0), N is N0 + 1.
	`
	if got := solveArith(t, prog, "len([a,b,c,d], N)", "N"); got != "4" {
		t.Errorf("len => %q, want 4", got)
	}
}

func TestFloatLiteralParses(t *testing.T) {
	tm, diags, err := ParseTerm("3.14")
	if err != nil {
		t.Fatalf("parse: %v (%v)", err, diags)
	}
	if f, ok := tm.(Float); !ok || f.Value != 3.14 {
		t.Fatalf("want Float 3.14, got %#v", tm)
	}
	if len(diags) != 0 {
		t.Errorf("float literal should not diagnose: %v", diags)
	}
}
