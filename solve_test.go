package prolog

import (
	"context"
	"testing"
)

// mustParse fails the test if the source produces syntax diagnostics.
func mustParse(t *testing.T, src string) *Program {
	t.Helper()
	prog := Parse(src)
	for _, d := range prog.Diagnostics {
		if d.Kind == DiagSyntax {
			t.Fatalf("unexpected syntax diagnostic: %s", d)
		}
	}
	return prog
}

func query(t *testing.T, src string) Term {
	t.Helper()
	g, _, err := ParseTerm(src)
	if err != nil {
		t.Fatalf("parse query %q: %v", src, err)
	}
	return g
}

// TestAncestorRecursion exercises the classic transitive-closure program:
// recursion, backtracking, and multiple answers.
func TestAncestorRecursion(t *testing.T) {
	prog := mustParse(t, `
		parent(tom, bob).
		parent(bob, ann).
		parent(ann, kim).
		ancestor(X, Y) :- parent(X, Y).
		ancestor(X, Y) :- parent(X, Z), ancestor(Z, Y).
	`)
	m := NewMachine(prog.Clauses)

	sols, err := m.Solve(context.Background(), []Term{query(t, "ancestor(tom, D)")}, 0)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sols {
		got[s["D"].String()] = true
	}
	for _, want := range []string{"bob", "ann", "kim"} {
		if !got[want] {
			t.Errorf("ancestor(tom, %s) not derived; got %v", want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("want 3 descendants, got %d: %v", len(got), got)
	}
}

// TestListMembership checks unification over list structure with a partial-list
// tail — genuine compound-term reasoning core cannot express.
func TestListMembership(t *testing.T) {
	prog := mustParse(t, `
		member(X, [X|_]).
		member(X, [_|T]) :- member(X, T).
	`)
	m := NewMachine(prog.Clauses)

	ok, err := m.Prove(context.Background(), []Term{query(t, "member(b, [a,b,c])")})
	if err != nil || !ok {
		t.Fatalf("member(b,[a,b,c]) should hold: ok=%v err=%v", ok, err)
	}
	no, err := m.Prove(context.Background(), []Term{query(t, "member(z, [a,b,c])")})
	if err != nil || no {
		t.Fatalf("member(z,[a,b,c]) should fail: ok=%v err=%v", no, err)
	}

	// Enumerate list elements by leaving the first argument unbound.
	sols, err := m.Solve(context.Background(), []Term{query(t, "member(E, [a,b,c])")}, 0)
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(sols) != 3 {
		t.Fatalf("want 3 members, got %d", len(sols))
	}
}

// TestOccursCheck confirms X = f(X) fails rather than building a cyclic term.
func TestOccursCheck(t *testing.T) {
	_, ok := Unify(Var{"X"}, Compound{Functor: "f", Args: []Term{Var{"X"}}}, Bindings{})
	if ok {
		t.Fatal("occurs-check failed: X = f(X) unified")
	}
}

// TestDepthBound proves left recursion aborts with ErrDepthExceeded instead of
// looping forever.
func TestDepthBound(t *testing.T) {
	prog := mustParse(t, `loop(X) :- loop(X).`)
	m := NewMachine(prog.Clauses, WithMaxDepth(64))
	_, err := m.Solve(context.Background(), []Term{query(t, "loop(a)")}, 0)
	if err != ErrDepthExceeded {
		t.Fatalf("want ErrDepthExceeded, got %v", err)
	}
}

// TestBuiltinUnifyAndNeq covers =/2 and \=/2.
func TestBuiltinUnifyAndNeq(t *testing.T) {
	m := NewMachine(nil)
	ok, _ := m.Prove(context.Background(), []Term{query(t, "a = a")})
	if !ok {
		t.Error("a = a should hold")
	}
	ok, _ = m.Prove(context.Background(), []Term{query(t, "a = b")})
	if ok {
		t.Error("a = b should fail")
	}
	ok, _ = m.Prove(context.Background(), []Term{query(t, "a \\= b")})
	if !ok {
		t.Error("a \\= b should hold")
	}
}
