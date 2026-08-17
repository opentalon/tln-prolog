# tln-prolog

[![CI](https://github.com/opentalon/tln-prolog/actions/workflows/ci.yml/badge.svg)](https://github.com/opentalon/tln-prolog/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**The Prolog runtime for [tln](https://github.com/opentalon/tln-language) — so a Prolog program can run in the tln world with no Prolog installed.**

The goal is **porting**: Prolog is the source, tln is the target. A `.pl`
program becomes a tln artifact that runs without an external Prolog system
(SWI, GNU, …). This repo is the piece that makes that possible for the *whole*
language, not just the easy subset.

## Why this exists

tln core is **flat-EAV, well-founded** — essentially Datalog. Datalog has **no
function symbols**, so it cannot represent Prolog's compound terms, lists, or the
unbounded term-building that makes full Prolog Turing-complete. That is a
provable limit, not a missing feature: the relational subset of a Prolog program
lowers to native tln rules, but `append/3`, difference lists, cut-dependent
control, `assert/retract`, and arithmetic do **not**.

`tln-prolog` is the **runtime backstop** for exactly that gap: a Prolog engine,
in pure Go, that lives inside the tln family. Ported clauses that cannot become
tln core rules run here instead — same ecosystem, still no external Prolog. It
carries the parts of the language core lacks:

- **Structured terms** — `Var · Atom · Int · Float · Compound`, lists as `"."/2` cells.
- **Unification** — most-general unifier with a sound, always-on occurs-check,
  over a trail-based substitution (O(1) bind, O(1) lookup, backtrack by undo).
- **Resolution** — a pure-Go SLD `Machine`: depth-first search, backtracking,
  fresh clause renaming, cut, and a depth bound that turns runaway recursion into
  an explicit `ErrDepthExceeded` instead of a hang.
- **Well-founded negation** — tabled predicates (`:- table`) are evaluated to the
  three-valued well-founded model, so left recursion terminates and `\+` means
  the same thing it does in tln core.
- **A `.pl` reader** — an ISO-subset parser that **never drops anything
  silently**: constructs it does not run come back as typed `Diagnostic`s.
- **A store boundary** — answers project to `[]factstore.Fact` so results flow
  into any tln FactStore (`MemoryStore`, `tln-db`, …).

## Where it sits in the port

```
                         ┌─ relational subset ──> native tln rules ──> tln core engine
  family.pl ── reader ──>┤                                                    │
   (this repo's parser)  └─ full-Prolog part ───> clauses on THIS engine ─────┤
                                                   (compound terms, cut,       │
                                                    assert, arithmetic, …)     ▼
                                                                        shared FactStore
```

The transpiler that decides the split, and the command a user runs to port a
file, are **separate repos**. This repo is deliberately just the two things the
port depends on: the **parser** (`.pl` → term IR) and the **engine** (the runtime
for everything that stays Prolog).

## What it runs

The engine runs a substantial ISO subset — enough to port real programs, not
just Horn clauses.

**Control & logic** — clauses, conjunction, backtracking, `true`/`fail`,
unification (`=`, `\=`), term comparison (`==`, `\==`, `@<`, `@>`, `@=<`, `@>=`,
`compare/3`), cut (`!`), if-then-else (`-> ;`), disjunction (`;`), negation
(`\+`, `not/1`), `once/1`:

```prolog
member(X, [X|_]).
member(X, [_|T]) :- member(X, T).

max(A, B, A) :- A >= B, !.
max(_, B, B).
```

**Arithmetic** — `is/2` and the comparison operators over an integer/float tower,
shared with tln core via the [`pkg/arith`](https://github.com/opentalon/tln-language/tree/master/pkg/arith)
kernel, so `X is 2 + 3 * 4` means exactly what it means in a tln rule:

```prolog
len([], 0).
len([_|T], N) :- len(T, N0), N is N0 + 1.
```

**All-solutions** — `findall/3`, `bagof/3`, `setof/3` with `^` existential
quantification and free-variable grouping; `copy_term/2`.

**Term / list / atom builtins** — `functor/3`, `arg/3`, `=../2`, `atom_length/2`,
`atom_codes/2`, `atom_chars/2`, `char_code/2`, `number_codes/2`, the type-test
family (`var`, `nonvar`, `atom`, `number`, `integer`, `float`, `compound`,
`is_list`, `ground`, …), and `sort/2` / `msort/2`. A bootstrap prelude adds the
usual list library — `append/3`, `member/2`, `reverse/2`, `length/2`, `nth0/3`,
`nth1/3`, `select/3`, `between/3`, … (`length`/`nth` are bidirectional).

**Exceptions** — `throw/1` and `catch/3`; arithmetic and database errors are
raised as standard ISO error balls (`instantiation_error`,
`type_error(evaluable, N/A)`, `evaluation_error(zero_divisor)`,
`permission_error(modify, static_procedure, N/A)`).

**Database** — run-scoped `assert/1`, `asserta/1`, `assertz/1`, `retract/1` on
predicates declared `:- dynamic name/arity`. Mutations live only for the query
that makes them (never touch the base program) and are recorded for audit;
mutating an undeclared predicate throws a `permission_error`.

**Tabling & well-founded semantics** — `:- table name/arity` evaluates a
predicate to its three-valued well-founded model (Van Gelder's alternating
fixpoint). Left recursion terminates and reachability is complete; non-stratified
negation gets a genuine *undefined* rather than a loop — the win/lose game is the
canonical case:

```prolog
:- table win/1.
move(a, b).  move(b, c).      % c is terminal
win(X) :- move(X, Y), \+ win(Y).
%  win(b) is true  (a win),
%  win(a), win(c) are false (losses).
%  A 2-cycle a<->b makes both undefined — a drawn game.
```

## What it still only diagnoses

A few constructs are recognised by the reader and **reported, not executed** —
the honest boundary:

| Diagnostic kind | Examples                                     |
|-----------------|----------------------------------------------|
| `io`            | `write/1`, `nl/0`, `read/1`, `format/*` — I/O is a host capability, not in the engine |
| `database`      | `retractall/1`, `abolish/1` (the bulk mutators; `assert`/`retract` run) |
| `unsupported`   | `forall/2`, `aggregate_all/3`                |
| `syntax`        | a malformed clause (skipped; reader resyncs) |

## The store boundary

When ported answers need to live in a tln FactStore, ground atoms project to EAV
facts under the `:pl/` namespace (the same shape `tln-asp` uses):

| Atom        | Fact                                                          |
|-------------|---------------------------------------------------------------|
| `p`         | `{RecordID: "p",       Attribute: ":pl/holds", Value: true}`  |
| `p(a)`      | `{RecordID: "a",       Attribute: ":pl/p",     Value: true}`  |
| `p(a,b,…)`  | `{RecordID: "p|a|b|…", Attribute: ":pl/p",     Value: [a,b,…]}`|

Crossing from structured terms back into flat EAV is **lossy by choice**: a
compound or list argument has no faithful scalar form, so it is reified to its
canonical string and a `Diagnostic` records it.

## The tln plugin family

`tln-prolog` is the **reasoner** leg, alongside
[`tln-db`](https://github.com/opentalon/tln-db) (a store),
[`tln-mcp`](https://github.com/opentalon/tln-mcp) (a tool), and
[`tln-asp`](https://github.com/opentalon/tln-asp) (a solver). Core stays a pure
language + planner + SPIs; every edge is a plugin. Design recorded as
[ADR 0009](https://github.com/opentalon/tln-language/blob/master/docs/design/0009-prolog-plugin.md);
the full-Prolog build-out is documented in
[docs/design/0001-full-prolog-completion.md](docs/design/0001-full-prolog-completion.md).

## Status

The full-Prolog roadmap (arithmetic, cut and control, `findall`/`bagof`/`setof`,
the term/list/atom builtins, `throw`/`catch`, run-scoped `assert`/`retract`, and
tabling with well-founded semantics) is **complete**. What remains is I/O (a host
capability, deliberately out of the engine), the bulk database mutators, and the
separate transpiler + porting CLI that live in their own repos.

## License

Apache 2.0 — see [LICENSE](LICENSE).
