# ADR 0001 — Completing tln-prolog to a full logic-programming engine

Status: proposed

## Context

Today the engine (`solve.go`) runs Horn clauses with `true`/`fail`, `=/2`,
`\=/2`, `,/2`, backtracking, and lists. Everything else — cut, arithmetic,
`findall`, `throw/catch`, database mutation, tabling — is **recognised by the
reader and refused at the boundary** as a typed `Diagnostic` (`reader.go`:
`DiagCut`, `DiagArith`, `DiagIO`, `DiagDatabase`, `DiagUnsupported`). That honest
boundary is the thing we now grow.

Two properties of the current implementation shape every step below:

- **CPS + flat goal list.** `Machine.solve(ctx, goals, s, depth, emit)` is a
  recursive continuation-passing resolver. When it expands a user clause it
  flattens `next := append(rc.Body, rest...)` — the current clause body and the
  caller's continuation become **one undifferentiated list**. Backtracking is the
  `for _, c := range m.clauses` loop; early exit is the `emit → stop bool` signal.
- **Immutable `Bindings`.** `Unify` clones the substitution map on each binding
  (`unify.go`); backtracking is just "keep the older `Bindings`". Correct, but
  O(n) per binding — relevant only when we reach tabling (Phase 6).

## Goal & contract

Reach "runs any reasonably-ported pure Prolog program" **without breaking the
reasoner contract** that justifies tln-prolog's existence (ADR 0009 in
tln-language):

- **No ambient side effects.** IO and any host reach-out is a *capability the
  host grants*, never something clause code takes. IO stays **out of this repo**
  (the `io` plugin owns it); the engine's output boundary remains `AtomFacts`.
- **Determinism & auditability per run.** A given program + facts yields the same
  result, and the resolution is traceable. This is the constraint that governs
  Phase 5b (database mutation).

## Completion pattern

Every pure phase is the same three-step move:

1. Implement the builtin(s) in `solve.go`'s goal switch (or add a term type in
   `term.go`).
2. Stop emitting the corresponding diagnostic in `reader.go`'s `scanDiagnostics`.
3. Add table-driven tests (`solve_test.go`) + a ported-program example.

The exception is **cut** (Phase 2), which needs an engine change, not a builtin.

## Phases

### Phase 1 — Arithmetic  *(low risk, unblocks ~everything)*

- Add `Float` to the term IR (`term.go`); the reader currently reads floats as
  atoms and flags them (`tFloat`). Wire `tFloat` through to `Float` and drop the
  diagnostic.
- Add an `eval(Term, Bindings) (number, error)` arithmetic evaluator: `+ - * //
  mod / abs min max`, integer/float tower.
- Builtins: `is/2`, `=:=/2`, `=\=/2`, `</2`, `>/2`, `>=/2`, `=</2`. Instantiation
  errors surface as `throw/catch` balls once Phase 5 lands; until then, as engine
  errors.
- Drop `DiagArith`.

### Phase 2 — Cut  *(shipped — approach (a), the barrier-marker)*

**Status: done.** `solve` now returns `(stop, commit *bool, err)`. Each clause
activation allocates a fresh `barrier := new(bool)`; `bindCut` rewrites `!` in the
selected body (recursing through the cut-transparent `,`/`;`/`->` constructs) to a
`cutMarker{barrier}`. When the cut's continuation is exhausted it returns
`barrier` as `commit`; every clause loop it unwinds through breaks without trying
alternatives, until the loop that owns that barrier consumes it — pruning both the
predicate's remaining clauses *and* the choice points of body goals before the
cut. Derived on top: `\+/1`, `not/1`, `once/1`, `(->)/2`, and `(->;)/3`
if-then-else, with `firstSol` as the cut-opaque boundary. `DiagCut` dropped; the
reader gained prefix `\+`.

<details><summary>Original design note (kept for context)</summary>

Cut cannot be "just another builtin", because the flat `body ++ rest` list has
already erased the boundary cut needs as its barrier. Two options:

- **(a) Barrier-marker goal.** When expanding a clause activation, allocate a
  fresh `cutSig := new(bool)`, rewrite `!` in that body to a `cutTo{cutSig}`
  pseudo-goal, and keep `rest` behind the barrier. `cutTo` succeeds, and on
  backtracking sets `*cutSig`. The predicate's clause loop checks `*cutSig` after
  each clause and `break`s. This reuses the existing `stop bool` plumbing — cut is
  a *scoped stop*. **Recommended: least invasive.**
- **(b) Framed goal stack.** Replace the flat list with frames that carry a
  barrier id, discarding choice points created between barrier and cut. Cleaner
  model, larger rewrite.

Open sub-problem to resolve in the Phase-2 PR: a `!` mid-body must also prevent
backtracking into body goals *before* it, not only stop the predicate's own
clause loop. Every choice point opened after the barrier and before the cut fires
must be pruned — so the signal has to be visible to those inner clause loops, not
just the outer one. Specify the exact plumbing before writing code.

Once cut exists, derive the rest as library/sugar: `(->)/2`, `(;)/2`, `\+/1`,
`once/1`, `not/1`. Drop `DiagCut`.

</details>

### Phase 3 — `findall/3` → `bagof/3` / `setof/3`

- `copy_term/2` first (term copy with fresh vars — reuse `rename` machinery).
- `findall(Tmpl, Goal, L)`: solve `Goal` to exhaustion, collect
  `copy_term(Resolve(Tmpl))` per solution, unify with `L`. `Solve` already
  collects; factor a sub-solve.
- `compare/3` + standard order of terms (`@<` etc.) — prerequisite for `setof`
  dedup/sort.
- `bagof`/`setof`: free-variable handling and `^/2` witness grouping.
- Drop the `findall`/`bagof`/`setof` `DiagUnsupported`.

### Phase 4 — Term / list / atom builtins + bootstrap prelude

- Go builtins: `functor/3`, `arg/3`, `=../2` (univ), `atom_codes/2`,
  `atom_chars/2`, `atom_length/2`, `char_code/2`, `number_codes/2`, type tests
  (`var/1 nonvar/1 atom/1 number/1 integer/1 compound/1 is_list/1`).
- Prelude `.pl` loaded into every machine: `append/3 member/2 length/2 reverse/2
  last/2 nth0/3 nth1/3 msort/2 sort/2 select/3 between/3`.

### Phase 5a — `throw/1` + `catch/3`

Propagate a `prologThrow{ball Term}` up the existing `error` return channel of
`solve`; `catch/3` recovers it, unifies the ball with the catcher (restoring
bindings to the catch point), and runs the recovery goal. Reuses plumbing that is
already there.

### Phase 5b — `assert`/`retract`  *(Bucket 3 — include, but gated; confirm design)*

This is the one item in genuine tension with the determinism/audit contract, so
it ships behind guardrails rather than as raw ISO mutation:

- **Opt-in only:** requires `:- dynamic p/N.` — mutation is visible, not implicit.
- **Run-scoped (decided):** `Machine.clauses` becomes copy-on-write per `Solve`;
  mutations live only for that run and are discarded at its end. There is no
  host-persistable mode — determinism-per-run holds and the run is replayable.
- **Traced:** every `assert`/`retract` is recorded in the resolution trace so the
  audit story survives.

Drop `DiagDatabase` only for declared dynamic predicates; keep it for undeclared
ones.

### Phase 6 — Tabling / well-founded semantics  *(strategic, hard)*

Memoize subgoals, suspend/resume on recursive calls, and for negation implement
the alternating-fixpoint / SCC simplification of delayed negative literals. This
is the step that makes `\+` mean what tln core's negation means (well-founded),
making tln-prolog semantically a tln citizen rather than merely ISO-ish. Expect
the immutable-`Bindings` clone cost to force a trail-based store here.

## Non-goals

- **IO in this repo.** `write/1`, `format/*`, `nl/0` route to the `io` plugin;
  the engine's own output stays `AtomFacts`. `DiagIO` remains.
- **Distribution / actors.** Completing the reasoner does **not** provide
  `spawn`/`Id@Node`/RPC. That gap is a separate decision (Web Prolog federation),
  explicitly out of scope here.

## Sequencing rationale

1 (arithmetic) unblocks nearly every real program at low risk. 2 (cut) gates
`->`/`;`/`\+`/`once`, so it comes before anything that wants them. 3–4 make ported
programs actually run. 5a is small and cheap. 5b is deferred behind the pure core
because it is the contract-tension item. 6 is the hard, strategic finish.
