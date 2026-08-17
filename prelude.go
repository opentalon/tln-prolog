package prolog

import "sync"

// preludeSrc is the bootstrap library loaded into every Machine: the pure
// list-manipulation predicates that are most naturally written in Prolog itself
// (structure builtins and sorting are Go-side, see builtins.go). It is parsed
// once and prepended to every clause database.
//
// length/2, nth0/3 and nth1/3 are bidirectional: they work whether the list or
// the index/count is the bound argument (integer index → deterministic; unbound
// → enumerate).
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

length(L, N) :- integer(N), !, N >= 0, length_build(N, L).
length(L, N) :- length_count(L, 0, N).
length_build(0, []) :- !.
length_build(N, [_|T]) :- N > 0, N1 is N - 1, length_build(N1, T).
length_count([], N, N).
length_count([_|T], N0, N) :- N1 is N0 + 1, length_count(T, N1, N).

nth0(N, L, X) :- integer(N), !, N >= 0, nth0_det(N, L, X).
nth0(N, L, X) :- nth_gen(L, X, 0, N).
nth0_det(0, [X|_], X) :- !.
nth0_det(N, [_|T], X) :- N > 0, N1 is N - 1, nth0_det(N1, T, X).

nth1(N, L, X) :- integer(N), !, N >= 1, N0 is N - 1, nth0_det(N0, L, X).
nth1(N, L, X) :- nth_gen(L, X, 1, N).

nth_gen([X|_], X, N, N).
nth_gen([_|T], X, N0, N) :- N1 is N0 + 1, nth_gen(T, X, N1, N).

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
