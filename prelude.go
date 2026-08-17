package prolog

import "sync"

// preludeSrc is the bootstrap library loaded into every Machine: the pure
// list-manipulation predicates that are most naturally written in Prolog itself
// (structure builtins and sorting are Go-side, see builtins.go). It is parsed
// once and prepended to every clause database.
//
// Note: length/2, nth0/3 and nth1/3 are written for the common measure/index
// direction (list given). The fully bidirectional forms are a later refinement.
const preludeSrc = `
append([], L, L).
append([H|T], L, [H|R]) :- append(T, L, R).

member(X, [X|_]).
member(X, [_|T]) :- member(X, T).

memberchk(X, L) :- member(X, L), !.

reverse(L, R) :- reverse_(L, [], R).
reverse_([], A, A).
reverse_([H|T], A, R) :- reverse_(T, [H|A], R).

last([X], X).
last([_|T], X) :- last(T, X).

length([], 0).
length([_|T], N) :- length(T, N0), N is N0 + 1.

nth0(0, [X|_], X).
nth0(N, [_|T], X) :- N > 0, N1 is N - 1, nth0(N1, T, X).

nth1(1, [X|_], X).
nth1(N, [_|T], X) :- N > 1, N1 is N - 1, nth1(N1, T, X).

select(X, [X|T], T).
select(X, [H|T], [H|R]) :- select(X, T, R).

between(L, H, L) :- L =< H.
between(L, H, X) :- L < H, L1 is L + 1, between(L1, H, X).
`

var (
	preludeOnce     sync.Once
	preludeCompiled []Clause
)

// prelude returns the parsed bootstrap clauses, compiled once.
func prelude() []Clause {
	preludeOnce.Do(func() {
		preludeCompiled = Parse(preludeSrc).Clauses
	})
	return preludeCompiled
}
