# tln-prolog

[![CI](https://github.com/opentalon/tln-prolog/actions/workflows/ci.yml/badge.svg)](https://github.com/opentalon/tln-prolog/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**A self-contained logic-programming reasoner plugin for [tln](https://github.com/opentalon/tln-language) — "tln as a new Prolog front-end."**

tln's core is a deterministic, **flat EAV** expert-system language: its
well-founded resolver yields a single three-valued model over facts whose terms
are only `Var | Lit` — no compound terms. Prolog's world is **structured**:
functor terms and lists, unification with occurs-check, and SLD resolution with
backtracking. Rather than push that into core, `tln-prolog` **brings its own
engine** — the same move [`tln-asp`](https://github.com/opentalon/tln-asp) made
for stable-model search.

`tln-prolog` is the tln plugin family's **reasoner** leg, alongside
[`tln-db`](https://github.com/opentalon/tln-db) (a **store**),
[`tln-mcp`](https://github.com/opentalon/tln-mcp) (a **tool**), and
[`tln-asp`](https://github.com/opentalon/tln-asp) (a **solver**). Core stays a
pure language + planner + SPIs; every edge is a plugin.

## What it is (and is not)

This repo is the **engine and the missing Prolog-language parts** core cannot
express:

- **Its own term IR** — `Var`, `Atom`, `Int`, `Compound` (functors), and lists
  as `"."/2` cells. tln's `factstore.Term` is only `Var | Lit`; the richer
  representation lives here, which makes the plugin *more* independent from core,
  not less.
- **Its own unifier** — most-general unification with a sound, always-on
  occurs-check.
- **Its own resolver** — a pure-Go SLD `Machine`: depth-first resolution,
  chronological backtracking, fresh clause renaming, and a depth bound that turns
  the classic left-recursion trap into an explicit `ErrDepthExceeded` instead of
  a hang.
- **Its own reader** — an ISO-subset `.pl` parser (facts, rules, conjunction,
  lists, a small operator table) that **never drops anything silently**:
  constructs the engine does not execute (cut `!`, IO, `assert`/`retract`,
  arithmetic) come back as typed `Diagnostic`s.
- **A store boundary** — `AtomFacts` projects ground atoms to
  `[]factstore.Fact`, so Prolog answers flow back into any tln FactStore.

It is **not** the transpiler and **not** the command line. The
`prolog2tln` transpiler and a Prolog-system CLI are **separate repos** that
*import* this engine. This package deliberately stays a library: parse → resolve
→ project, with the diagnostics a transpiler needs to decide what maps to tln and
what is lossy.

## Engine end to end

```go
import (
    "context"
    prolog "github.com/opentalon/tln-prolog"
)

prog := prolog.Parse(`
    parent(tom, bob).
    parent(bob, ann).
    ancestor(X, Y) :- parent(X, Y).
    ancestor(X, Y) :- parent(X, Z), ancestor(Z, Y).
`)

goal, _, _ := prolog.ParseTerm("ancestor(tom, D)")
sols, _ := prolog.NewMachine(prog.Clauses).Solve(context.Background(), []prolog.Term{goal}, 0)
for _, s := range sols {
    fmt.Println("D =", s["D"]) // bob, ann
}
```

Lists and partial-list tails work the way you would expect — real compound-term
reasoning core cannot express:

```prolog
member(X, [X|_]).
member(X, [_|T]) :- member(X, T).
```

## The store boundary

Answers project back to EAV facts a host can assert into a tln FactStore
(`MemoryStore`, `tln-db`, …). The encoding mirrors `tln-asp`, under the `:pl/`
namespace:

| Atom          | Fact                                                        |
|---------------|-------------------------------------------------------------|
| `p`           | `{RecordID: "p",     Attribute: ":pl/holds", Value: true}`  |
| `p(a)`        | `{RecordID: "a",     Attribute: ":pl/p",     Value: true}`  |
| `p(a,b,…)`    | `{RecordID: "p|a|b|…", Attribute: ":pl/p",   Value: [a,b,…]}` |

```go
atoms := make([]prolog.Term, len(sols))
for i, s := range sols {
    atoms[i] = prolog.Instantiate(goal, s) // ?- p(X) + {X=a} -> p(a)
}
facts, diags := prolog.AtomFacts(atoms)
store.Assert(ctx, facts)
```

Crossing from structured terms back into flat EAV is **lossy by choice**: a
compound or list argument has no faithful scalar form, so it is reified to its
canonical Prolog string and a `Diagnostic` records it. A host that needs full
term structure works with `Term` / `Solution` directly instead. Same trade-off
`tln-asp` makes at its atoms→facts boundary.

## The honest boundary

The engine runs **pure Horn-clause logic** plus the control built-ins `true`,
`fail`, `=/2`, and `\=/2`. Everything else Prolog-ish is recognised by the reader
and reported, not executed:

| Diagnostic kind | Examples                                    |
|-----------------|---------------------------------------------|
| `cut`           | `!`                                         |
| `io`            | `write/1`, `nl/0`, `read/1`, `format/*`     |
| `database`      | `assert/1`, `asserta`, `assertz`, `retract` |
| `arith`         | `is/2`, `</2`, `>/2`, `=:=/2`               |
| `unsupported`   | `findall/3`, `setof/3`, float literals      |
| `syntax`        | a malformed clause (skipped; reader resyncs)|

This is the surface a future `prolog2tln` transpiler builds on: the runnable
subset maps to tln; the diagnostics say what a Prolog program uses that a
deterministic EAV target cannot faithfully carry.

## Design

Recorded as an ADR in the tln-language repo:
[ADR 0009 — Prolog as an external reasoner plugin](https://github.com/opentalon/tln-language/blob/master/docs/design/0009-prolog-plugin.md).

Roadmap (out of scope for this repo): tabling for terminating left recursion, an
arithmetic evaluator behind a flag, the `prolog2tln` transpiler, and a Prolog CLI
— each a separate plugin that imports this engine.

## License

Apache 2.0 — see [LICENSE](LICENSE).
