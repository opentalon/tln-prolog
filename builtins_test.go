package prolog

import (
	"context"
	"testing"
)

// first returns the first solution's binding for wantVar, or "<none>".
func first(t *testing.T, program, goal, wantVar string) string {
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

func TestFunctor(t *testing.T) {
	if got := first(t, "", "functor(foo(a,b,c), N, A)", "N"); got != "foo" {
		t.Errorf("functor name => %q, want foo", got)
	}
	if got := first(t, "", "functor(foo(a,b,c), N, A)", "A"); got != "3" {
		t.Errorf("functor arity => %q, want 3", got)
	}
	if got := first(t, "", "functor(T, point, 2)", "T"); got != "point(_G1,_G2)" {
		t.Errorf("functor construct => %q, want point(_G1,_G2)", got)
	}
	if got := first(t, "", "functor(hello, N, A)", "A"); got != "0" {
		t.Errorf("functor atom arity => %q, want 0", got)
	}
}

func TestArg(t *testing.T) {
	if got := first(t, "", "arg(2, foo(a,b,c), X)", "X"); got != "b" {
		t.Errorf("arg => %q, want b", got)
	}
	if got := first(t, "", "arg(9, foo(a), X)", "X"); got != "<none>" {
		t.Errorf("arg out of range => %q, want <none>", got)
	}
}

func TestUniv(t *testing.T) {
	if got := first(t, "", "foo(a,b) =.. L", "L"); got != "[foo,a,b]" {
		t.Errorf("univ decompose => %q, want [foo,a,b]", got)
	}
	if got := first(t, "", "T =.. [point, 1, 2]", "T"); got != "point(1,2)" {
		t.Errorf("univ construct => %q, want point(1,2)", got)
	}
	if got := first(t, "", "T =.. [hello]", "T"); got != "hello" {
		t.Errorf("univ atom => %q, want hello", got)
	}
}

func TestAtomConversions(t *testing.T) {
	if got := first(t, "", "atom_length(hello, N)", "N"); got != "5" {
		t.Errorf("atom_length => %q, want 5", got)
	}
	if got := first(t, "", "atom_codes(abc, L)", "L"); got != "[97,98,99]" {
		t.Errorf("atom_codes => %q, want [97,98,99]", got)
	}
	if got := first(t, "", "atom_codes(A, [97,98,99])", "A"); got != "abc" {
		t.Errorf("atom_codes reverse => %q, want abc", got)
	}
	if got := first(t, "", "atom_chars(cat, L)", "L"); got != "[c,a,t]" {
		t.Errorf("atom_chars => %q, want [c,a,t]", got)
	}
	if got := first(t, "", "char_code(z, C)", "C"); got != "122" {
		t.Errorf("char_code => %q, want 122", got)
	}
	if got := first(t, "", "number_codes(N, [52,50])", "N"); got != "42" {
		t.Errorf("number_codes => %q, want 42", got)
	}
}

func TestTypeTests(t *testing.T) {
	proveTrue(t, "", "atom(foo)")
	proveTrue(t, "", "integer(42)")
	proveTrue(t, "", "float(3.14)")
	proveTrue(t, "", "number(42)")
	proveTrue(t, "", "compound(foo(x))")
	proveTrue(t, "", "is_list([a,b,c])")
	proveTrue(t, "", "ground(foo(a,b))")
	proveTrue(t, "", "var(_)")
	proveFalse(t, "", "atom(42)")
	proveFalse(t, "", "is_list([a|_])")
	proveFalse(t, "", "ground(foo(_))")
	proveFalse(t, "", "var(bound)")
}

func TestSortBuiltins(t *testing.T) {
	if got := first(t, "", "msort([3,1,2,1], L)", "L"); got != "[1,1,2,3]" {
		t.Errorf("msort => %q, want [1,1,2,3]", got)
	}
	if got := first(t, "", "sort([3,1,2,1], L)", "L"); got != "[1,2,3]" {
		t.Errorf("sort => %q, want [1,2,3]", got)
	}
}

func TestPreludeListPredicates(t *testing.T) {
	if got := first(t, "", "append([a,b], [c,d], L)", "L"); got != "[a,b,c,d]" {
		t.Errorf("append => %q, want [a,b,c,d]", got)
	}
	if got := first(t, "", "reverse([1,2,3], L)", "L"); got != "[3,2,1]" {
		t.Errorf("reverse => %q, want [3,2,1]", got)
	}
	if got := first(t, "", "length([a,b,c,d], N)", "N"); got != "4" {
		t.Errorf("length => %q, want 4", got)
	}
	if got := first(t, "", "last([a,b,c], X)", "X"); got != "c" {
		t.Errorf("last => %q, want c", got)
	}
	if got := first(t, "", "nth0(1, [a,b,c], X)", "X"); got != "b" {
		t.Errorf("nth0 => %q, want b", got)
	}
	if got := first(t, "", "nth1(1, [a,b,c], X)", "X"); got != "a" {
		t.Errorf("nth1 => %q, want a", got)
	}
	proveTrue(t, "", "member(b, [a,b,c])")
	proveTrue(t, "", "memberchk(b, [a,b,c])")
}

func TestBetween(t *testing.T) {
	// between/3 enumerates the inclusive range.
	prog := Parse("")
	g, _, err := ParseTerm("between(1, 4, X)")
	if err != nil {
		t.Fatal(err)
	}
	sols, err := NewMachine(prog.Clauses).Solve(context.Background(), []Term{g}, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(sols))
	for i, s := range sols {
		got[i] = s["X"].String()
	}
	if !eq(got, []string{"1", "2", "3", "4"}) {
		t.Errorf("between(1,4,X) => %v, want [1 2 3 4]", got)
	}
}

// A user definition of a prelude predicate shadows the prelude entirely.
func TestUserShadowsPrelude(t *testing.T) {
	prog := `member(only, [only]).`
	g, _, err := ParseTerm("member(X, [a,b,c])")
	if err != nil {
		t.Fatal(err)
	}
	sols, err := NewMachine(Parse(prog).Clauses).Solve(context.Background(), []Term{g}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// User's member/2 only matches member(only,[only]); it does not enumerate.
	if len(sols) != 0 {
		t.Errorf("user member should shadow prelude; got %d solutions", len(sols))
	}
}
