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

- **Structured terms** — `Var · Atom · Int · Compound`, lists as `"."/2` cells.
- **Unification** — most-general unifier with a sound, always-on occurs-check.
- **Resolution** — a pure-Go SLD `Machine`: depth-first search, backtracking,
  fresh clause renaming, and a depth bound that turns left recursion into an
  explicit `ErrDepthExceeded` instead of a hang.
- **A `.pl` reader** — an ISO-subset parser that **never drops anything
  silently**: constructs it does not yet run come back as typed `Diagnostic`s.
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

## What it runs today, and what it only diagnoses

The engine runs **Horn-clause logic** plus the control built-ins `true`, `fail`,
`=/2`, and `\=/2` — enough for recursion, lists, and backtracking:

```prolog
member(X, [X|_]).
member(X, [_|T]) :- member(X, T).

ancestor(X, Y) :- parent(X, Y).
ancestor(X, Y) :- parent(X, Z), ancestor(Z, Y).
```

Everything else Prolog-ish is recognised by the reader and **reported, not yet
executed** — this is the honest boundary, and the checklist of what the engine
must gain to run *any* program:

| Diagnostic kind | Examples                                    |
|-----------------|---------------------------------------------|
| `cut`           | `!`                                         |
| `io`            | `write/1`, `nl/0`, `read/1`, `format/*`     |
| `database`      | `assert/1`, `asserta`, `assertz`, `retract` |
| `arith`         | `is/2`, `</2`, `>/2`, `=:=/2`               |
| `unsupported`   | `findall/3`, `setof/3`, float literals      |
| `syntax`        | a malformed clause (skipped; reader resyncs)|

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
[ADR 0009](https://github.com/opentalon/tln-language/blob/master/docs/design/0009-prolog-plugin.md).

## Roadmap

To run *any* ported program, the engine grows the parts it currently only
diagnoses — cut, arithmetic evaluation, `\+`, `findall/bagof/setof`,
`assert/retract`, IO, and the standard term/list builtins — plus tabling for
terminating left recursion. The transpiler and the porting command line live in
their own repos.

## License

Apache 2.0 — see [LICENSE](LICENSE).
