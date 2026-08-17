package prolog

import "testing"

func TestParseFactsAndRule(t *testing.T) {
	prog := Parse(`
		% edges
		edge(a, b).
		edge(b, c).
		path(X, Y) :- edge(X, Y).
		path(X, Y) :- edge(X, Z), path(Z, Y).
	`)
	if len(prog.Clauses) != 4 {
		t.Fatalf("want 4 clauses, got %d", len(prog.Clauses))
	}
	// The recursive rule must flatten its body conjunction into two goals.
	last := prog.Clauses[3]
	if len(last.Body) != 2 {
		t.Fatalf("want 2 body goals, got %d: %s", len(last.Body), last)
	}
}

func TestParseListSyntax(t *testing.T) {
	tm, diags, err := ParseTerm("append([a,b|T], Ys)")
	if err != nil {
		t.Fatalf("parse: %v (%v)", err, diags)
	}
	c, ok := tm.(Compound)
	if !ok || c.Functor != "append" || len(c.Args) != 2 {
		t.Fatalf("bad parse: %s", tm)
	}
	// First arg is a "."/2 cons cell with a variable tail.
	if got := c.Args[0].String(); got != "[a,b|T]" {
		t.Fatalf("list render mismatch: %s", got)
	}
}

func TestDiagnosticsForUnsupported(t *testing.T) {
	prog := Parse(`
		go :- write(hello), nl, X is 1 + 2, assert(seen), !.
	`)
	kinds := map[DiagnosticKind]int{}
	for _, d := range prog.Diagnostics {
		kinds[d.Kind]++
	}
	for _, want := range []DiagnosticKind{DiagIO, DiagDatabase, DiagCut} {
		if kinds[want] == 0 {
			t.Errorf("expected a %s diagnostic; got %v", want, prog.Diagnostics)
		}
	}
	// Arithmetic is now evaluated, so `is/2` must NOT be diagnosed.
	if kinds[DiagArith] != 0 {
		t.Errorf("arithmetic is evaluated now; unexpected arith diagnostic: %v", prog.Diagnostics)
	}
	// Despite the unsupported goals, the clause is still read.
	if len(prog.Clauses) != 1 {
		t.Fatalf("clause should still parse, got %d", len(prog.Clauses))
	}
}

func TestSyntaxErrorIsResynced(t *testing.T) {
	// First clause is malformed (missing ')'), second is fine. Reader should
	// recover and still return the good clause.
	prog := Parse(`broken(a, b. ok(x).`)
	var syntax int
	for _, d := range prog.Diagnostics {
		if d.Kind == DiagSyntax {
			syntax++
		}
	}
	if syntax == 0 {
		t.Fatal("expected a syntax diagnostic")
	}
	found := false
	for _, c := range prog.Clauses {
		if c.Head.String() == "ok(x)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reader did not recover to parse ok(x): %+v", prog.Clauses)
	}
}
