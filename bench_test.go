package prolog

import (
	"context"
	"testing"
)

// BenchmarkNaiveReverse exercises the substitution under heavy binding churn:
// naive reverse of an N-element list is O(N²) appends, each binding and undoing
// many variables — the workload the trail store optimises versus clone-on-write.
func BenchmarkNaiveReverse(b *testing.B) {
	src := `
		app([], L, L).
		app([H|T], L, [H|R]) :- app(T, L, R).
		nrev([], []).
		nrev([H|T], R) :- nrev(T, RT), app(RT, [H], R).
	`
	m := NewMachine(Parse(src).Clauses)
	// A 30-element list.
	items := make([]Term, 30)
	for i := range items {
		items[i] = Int{int64(i)}
	}
	goal := Compound{Functor: "nrev", Args: []Term{List(items, Nil), Var{"R"}}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Solve(context.Background(), []Term{goal}, 1); err != nil {
			b.Fatal(err)
		}
	}
}
