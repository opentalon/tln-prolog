package prolog

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/opentalon/tln-language/pkg/arith"
)

// Clause is a program clause Head :- Body. A fact has an empty Body. Bodies are
// stored as a conjunction (a slice of goals); the reader flattens ","/2.
type Clause struct {
	Head Term
	Body []Term
}

func (c Clause) String() string {
	if len(c.Body) == 0 {
		return c.Head.String() + "."
	}
	parts := make([]string, len(c.Body))
	for i, g := range c.Body {
		parts[i] = g.String()
	}
	out := c.Head.String() + " :- "
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out + "."
}

// ErrDepthExceeded is returned when resolution hits the [Machine] depth bound,
// signalling that the search was cut off rather than exhausted. It surfaces the
// classic left-recursion trap instead of hanging.
var ErrDepthExceeded = errors.New("prolog: SLD resolution depth bound exceeded")

// prologThrow carries a thrown ball up the error channel of solve. catch/3
// intercepts it; if it reaches Solve uncaught it surfaces as the query error.
type prologThrow struct{ ball Term }

func (e prologThrow) Error() string { return "prolog: uncaught exception: " + e.ball.String() }

// Ball returns the thrown term of an uncaught prolog exception, and ok=true, so
// hosts can inspect it. Any other error returns ok=false.
func Ball(err error) (Term, bool) {
	if pt, ok := err.(prologThrow); ok {
		return pt.ball, true
	}
	return nil, false
}

// The ISO error terms thrown by builtins: error(Formal, Context).
func errorBall(formal Term) error {
	return prologThrow{Compound{Functor: "error", Args: []Term{formal, Var{"_"}}}}
}
func instantiationError() error { return errorBall(Atom{"instantiation_error"}) }
func typeError(kind string, culprit Term) error {
	return errorBall(Compound{Functor: "type_error", Args: []Term{Atom{kind}, culprit}})
}
func evaluationError(what string) error {
	return errorBall(Compound{Functor: "evaluation_error", Args: []Term{Atom{what}}})
}

// Machine is a pure-Go SLD-resolution engine over a set of clauses. It performs
// depth-first resolution with chronological backtracking and an occurs-checking
// unifier. A configurable depth bound guards against non-terminating left
// recursion; tabling (memoization of ground subgoals) is a planned follow-up and
// intentionally not claimed here.
type Machine struct {
	clauses  []Clause
	maxDepth int
	renameCt int
}

// Option configures a [Machine].
type Option func(*Machine)

// WithMaxDepth sets the SLD resolution depth bound (default 4096). Reaching it
// aborts the query with [ErrDepthExceeded].
func WithMaxDepth(n int) Option { return func(m *Machine) { m.maxDepth = n } }

// NewMachine builds a resolver over clauses, with the bootstrap prelude
// (append/3, member/2, …) prepended so those library predicates are available.
// A user definition of a prelude predicate shadows the prelude one entirely, so
// redefining e.g. member/2 does not double its solutions.
func NewMachine(clauses []Clause, opts ...Option) *Machine {
	userPreds := make(map[string]bool, len(clauses))
	for _, c := range clauses {
		userPreds[Indicator(c.Head)] = true
	}
	pre := prelude()
	all := make([]Clause, 0, len(pre)+len(clauses))
	for _, c := range pre {
		if userPreds[Indicator(c.Head)] {
			continue // user definition wins
		}
		all = append(all, c)
	}
	all = append(all, clauses...)
	m := &Machine{clauses: all, maxDepth: 4096}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Clauses returns the machine's clause database.
func (m *Machine) Clauses() []Clause { return m.clauses }

// Solution is a resolved answer: the query variables bound to ground (or
// partially instantiated) terms.
type Solution map[string]Term

// Solve proves the goal conjunction and returns up to maxSolutions answers
// (maxSolutions <= 0 means all). Each [Solution] maps the variables occurring in
// goals to their resolved values. Solutions come back in resolution order.
func (m *Machine) Solve(ctx context.Context, goals []Term, maxSolutions int) ([]Solution, error) {
	qvars := queryVars(goals)
	var out []Solution
	m.renameCt = 0

	_, _, err := m.solve(ctx, goals, Bindings{}, 0, func(s Bindings) bool {
		sol := make(Solution, len(qvars))
		for _, v := range qvars {
			sol[v] = Resolve(Var{v}, s)
		}
		out = append(out, sol)
		return maxSolutions > 0 && len(out) >= maxSolutions
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// Prove reports whether the goal conjunction has at least one solution.
func (m *Machine) Prove(ctx context.Context, goals []Term) (bool, error) {
	sols, err := m.Solve(ctx, goals, 1)
	return len(sols) > 0, err
}

// solve is the recursive SLD engine. emit is called with each complete
// substitution and returns true to stop the search early.
//
// It returns three signals. stop propagates an early stop from emit (the
// solution cap) — unwind everything. commit carries a cut: when a '!' commits,
// it returns the barrier pointer of the clause activation it belongs to, and
// each clause loop it unwinds through breaks without trying alternatives until
// the loop that owns that barrier consumes it. err is a hard failure.
func (m *Machine) solve(ctx context.Context, goals []Term, s Bindings, depth int, emit func(Bindings) bool) (stop bool, commit *bool, err error) {
	if e := ctx.Err(); e != nil {
		return false, nil, e
	}
	if len(goals) == 0 {
		return emit(s), nil, nil
	}
	if depth > m.maxDepth {
		return false, nil, ErrDepthExceeded
	}

	goal := walk(goals[0], s)
	rest := goals[1:]

	switch g := goal.(type) {
	case cutMarker:
		// Cut succeeds and lets the continuation run; once that continuation is
		// exhausted, commit to this cut's barrier so the owning clause loop
		// stops offering alternatives (and nested loops in between unwind).
		stop, commit, err := m.solve(ctx, rest, s, depth+1, emit)
		if err != nil || stop || commit != nil {
			return stop, commit, err
		}
		return false, g.barrier, nil
	case Atom:
		switch g.Name {
		case "true":
			return m.solve(ctx, rest, s, depth+1, emit)
		case "fail", "false":
			return false, nil, nil
		case "!":
			// An unbound cut (typed at the top level, no clause choice point to
			// prune) behaves as true.
			return m.solve(ctx, rest, s, depth+1, emit)
		}
	case Compound:
		switch {
		case g.Functor == "," && len(g.Args) == 2:
			return m.solve(ctx, append([]Term{g.Args[0], g.Args[1]}, rest...), s, depth+1, emit)
		case g.Functor == ";" && len(g.Args) == 2:
			return m.solveOr(ctx, g.Args[0], g.Args[1], rest, s, depth, emit)
		case g.Functor == "->" && len(g.Args) == 2:
			return m.solveITE(ctx, g.Args[0], g.Args[1], nil, rest, s, depth, emit)
		case (g.Functor == "\\+" || g.Functor == "not") && len(g.Args) == 1:
			ok, err := m.succeeds(ctx, g.Args[0], s, depth)
			if err != nil {
				return false, nil, err
			}
			if ok {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s, depth+1, emit)
		case g.Functor == "once" && len(g.Args) == 1:
			b, ok, err := m.firstSol(ctx, g.Args[0], s, depth)
			if err != nil {
				return false, nil, err
			}
			if !ok {
				return false, nil, nil
			}
			return m.solve(ctx, rest, b, depth+1, emit)
		case g.Functor == "throw" && len(g.Args) == 1:
			return false, nil, prologThrow{ball: m.copyOut(g.Args[0], s)}
		case g.Functor == "catch" && len(g.Args) == 3:
			return m.solveCatch(ctx, g.Args[0], g.Args[1], g.Args[2], rest, s, depth, emit)
		case g.Functor == "=" && len(g.Args) == 2:
			s2, ok := Unify(g.Args[0], g.Args[1], s)
			if !ok {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s2, depth+1, emit)
		case g.Functor == "\\=" && len(g.Args) == 2:
			if _, ok := Unify(g.Args[0], g.Args[1], s); ok {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s, depth+1, emit)
		case g.Functor == "is" && len(g.Args) == 2:
			v, err := evalArith(g.Args[1], s)
			if err != nil {
				return false, nil, err
			}
			s2, ok := Unify(g.Args[0], numTerm(v), s)
			if !ok {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s2, depth+1, emit)
		case isArithCompare(g.Functor) && len(g.Args) == 2:
			a, err := evalArith(g.Args[0], s)
			if err != nil {
				return false, nil, err
			}
			b, err := evalArith(g.Args[1], s)
			if err != nil {
				return false, nil, err
			}
			if !compareHolds(g.Functor, arith.Compare(a, b)) {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s, depth+1, emit)
		case isTermCompare(g.Functor) && len(g.Args) == 2:
			if !termCompareHolds(g.Functor, compareTerms(Resolve(g.Args[0], s), Resolve(g.Args[1], s))) {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s, depth+1, emit)
		case g.Functor == "compare" && len(g.Args) == 3:
			ord := []string{"<", "=", ">"}[compareTerms(Resolve(g.Args[1], s), Resolve(g.Args[2], s))+1]
			s2, ok := Unify(g.Args[0], Atom{ord}, s)
			if !ok {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s2, depth+1, emit)
		case g.Functor == "copy_term" && len(g.Args) == 2:
			s2, ok := Unify(g.Args[1], m.copyOut(g.Args[0], s), s)
			if !ok {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s2, depth+1, emit)
		case g.Functor == "findall" && len(g.Args) == 3:
			return m.solveFindall(ctx, g.Args[0], g.Args[1], g.Args[2], rest, s, depth, emit)
		case g.Functor == "bagof" && len(g.Args) == 3:
			return m.solveBagof(ctx, g.Args[0], g.Args[1], g.Args[2], rest, s, depth, emit, false)
		case g.Functor == "setof" && len(g.Args) == 3:
			return m.solveBagof(ctx, g.Args[0], g.Args[1], g.Args[2], rest, s, depth, emit, true)
		case g.Functor == "functor" && len(g.Args) == 3:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return m.biFunctor(g.Args[0], g.Args[1], g.Args[2], s) })
		case g.Functor == "arg" && len(g.Args) == 3:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return biArg(g.Args[0], g.Args[1], g.Args[2], s) })
		case g.Functor == "=.." && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return m.biUniv(g.Args[0], g.Args[1], s) })
		case g.Functor == "atom_length" && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return biAtomLength(g.Args[0], g.Args[1], s) })
		case g.Functor == "atom_codes" && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return m.biAtomText(g.Args[0], g.Args[1], s, false, false) })
		case g.Functor == "atom_chars" && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return m.biAtomText(g.Args[0], g.Args[1], s, true, false) })
		case g.Functor == "number_codes" && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return m.biAtomText(g.Args[0], g.Args[1], s, false, true) })
		case g.Functor == "char_code" && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return biCharCode(g.Args[0], g.Args[1], s) })
		case g.Functor == "msort" && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return biSort(g.Args[0], g.Args[1], s, false) })
		case g.Functor == "sort" && len(g.Args) == 2:
			return m.det(ctx, rest, s, depth, emit, func() (Bindings, bool) { return biSort(g.Args[0], g.Args[1], s, true) })
		case isTypeTest(g.Functor) && len(g.Args) == 1:
			if !typeTestHolds(g.Functor, g.Args[0], s) {
				return false, nil, nil
			}
			return m.solve(ctx, rest, s, depth+1, emit)
		}
	}

	// User clauses: try each, renamed fresh so its variables never clash. A
	// fresh barrier scopes any '!' in the selected clause body to this call.
	barrier := new(bool)
	for _, c := range m.clauses {
		rc := m.rename(c)
		s2, ok := Unify(goal, rc.Head, s)
		if !ok {
			continue
		}
		body := bindCutGoals(rc.Body, barrier)
		next := append(append([]Term{}, body...), rest...)
		stop, commit, err := m.solve(ctx, next, s2, depth+1, emit)
		if err != nil {
			return false, nil, err
		}
		if stop {
			return true, nil, nil
		}
		if commit != nil {
			if commit == barrier {
				return false, nil, nil // cut committed to this predicate: stop
			}
			return false, commit, nil // a cut for an outer barrier: propagate
		}
	}
	return false, nil, nil
}

// solveOr handles disjunction (A ; B), including the (Cond -> Then ; Else)
// if-then-else idiom.
func (m *Machine) solveOr(ctx context.Context, left, right Term, rest []Term, s Bindings, depth int, emit func(Bindings) bool) (bool, *bool, error) {
	if ite, ok := left.(Compound); ok && ite.Functor == "->" && len(ite.Args) == 2 {
		return m.solveITE(ctx, ite.Args[0], ite.Args[1], right, rest, s, depth, emit)
	}
	stop, commit, err := m.solve(ctx, append([]Term{left}, rest...), s, depth+1, emit)
	if err != nil || stop || commit != nil {
		return stop, commit, err
	}
	return m.solve(ctx, append([]Term{right}, rest...), s, depth+1, emit)
}

// solveITE handles (Cond -> Then) and (Cond -> Then ; Else). Cond is committed
// to its first solution (a local cut); elseGoal is nil for a bare if-then.
func (m *Machine) solveITE(ctx context.Context, cond, then, elseGoal Term, rest []Term, s Bindings, depth int, emit func(Bindings) bool) (bool, *bool, error) {
	b, ok, err := m.firstSol(ctx, cond, s, depth)
	if err != nil {
		return false, nil, err
	}
	if ok {
		return m.solve(ctx, append([]Term{then}, rest...), b, depth+1, emit)
	}
	if elseGoal == nil {
		return false, nil, nil
	}
	return m.solve(ctx, append([]Term{elseGoal}, rest...), s, depth+1, emit)
}

// det runs a deterministic builtin: f produces the extended bindings (or ok=
// false to fail), then resolution continues with the rest of the goals.
func (m *Machine) det(ctx context.Context, rest []Term, s Bindings, depth int, emit func(Bindings) bool, f func() (Bindings, bool)) (bool, *bool, error) {
	s2, ok := f()
	if !ok {
		return false, nil, nil
	}
	return m.solve(ctx, rest, s2, depth+1, emit)
}

// solveCatch implements catch/3. It runs Goal, streaming each solution into the
// continuation (rest); an exception thrown *within Goal* whose ball unifies with
// Catcher runs Recovery instead. Exceptions from the continuation, and non-throw
// errors (depth bound, cancellation), pass through uncaught.
func (m *Machine) solveCatch(ctx context.Context, goal, catcher, recovery Term, rest []Term, s Bindings, depth int, emit func(Bindings) bool) (bool, *bool, error) {
	var contStop bool
	var contCommit *bool
	var contErr error
	contRan := false

	// Solve Goal alone; for each of its solutions, run the continuation. The
	// continuation's outcome is captured here so it is not subject to this catch.
	gStop, gCommit, gErr := m.solve(ctx, []Term{goal}, s, depth+1, func(b Bindings) bool {
		contRan = true
		contStop, contCommit, contErr = m.solve(ctx, rest, b, depth+1, emit)
		return contStop || contCommit != nil || contErr != nil
	})

	// A stop/commit/error from the continuation takes priority and escapes catch.
	if contRan && (contErr != nil || contCommit != nil || contStop) {
		return contStop, contCommit, contErr
	}
	// Goal's own outcome.
	if gErr != nil {
		if pt, ok := gErr.(prologThrow); ok {
			s2, ok := Unify(catcher, pt.ball, s)
			if !ok {
				return false, nil, gErr // catcher mismatch: rethrow
			}
			return m.solve(ctx, append([]Term{recovery}, rest...), s2, depth+1, emit)
		}
		return false, nil, gErr // depth bound / cancellation: propagate
	}
	if gStop {
		return true, nil, nil
	}
	return false, gCommit, nil
}

// succeeds reports whether goal has at least one solution under s.
func (m *Machine) succeeds(ctx context.Context, goal Term, s Bindings, depth int) (bool, error) {
	_, ok, err := m.firstSol(ctx, goal, s, depth)
	return ok, err
}

// firstSol solves goal to its first solution, returning the extended bindings.
// It is the cut-opaque boundary used by \+, once, and the condition of ->.
func (m *Machine) firstSol(ctx context.Context, goal Term, s Bindings, depth int) (Bindings, bool, error) {
	var found Bindings
	ok := false
	_, _, err := m.solve(ctx, []Term{goal}, s, depth+1, func(b Bindings) bool {
		found = b
		ok = true
		return true // stop at the first solution
	})
	return found, ok, err
}

// cutMarker is the internal goal that '!' becomes once bound to the cut barrier
// of the clause activation it appears in.
type cutMarker struct{ barrier *bool }

func (cutMarker) isTerm()        {}
func (cutMarker) String() string { return "!" }

// bindCutGoals binds every '!' in a clause body to barrier.
func bindCutGoals(body []Term, barrier *bool) []Term {
	out := make([]Term, len(body))
	for i, g := range body {
		out[i] = bindCut(g, barrier)
	}
	return out
}

// bindCut replaces '!' with a cutMarker bound to barrier, recursing through the
// control constructs (,/;/->) that are transparent to cut so a cut inside a
// branch still cuts the enclosing clause. It does not descend into ordinary
// predicate arguments.
func bindCut(g Term, barrier *bool) Term {
	if a, ok := g.(Atom); ok && a.Name == "!" {
		return cutMarker{barrier}
	}
	if c, ok := g.(Compound); ok && len(c.Args) == 2 {
		switch c.Functor {
		case ",", ";", "->":
			return Compound{Functor: c.Functor, Args: []Term{
				bindCut(c.Args[0], barrier),
				bindCut(c.Args[1], barrier),
			}}
		}
	}
	return g
}

// rename returns a copy of c with every variable given a fresh, activation-unique
// name so a clause can be used many times in one proof without capture.
func (m *Machine) rename(c Clause) Clause {
	m.renameCt++
	suffix := fmt.Sprintf("#%d", m.renameCt)
	seen := map[string]string{}
	return Clause{
		Head: renameTerm(c.Head, suffix, seen),
		Body: renameGoals(c.Body, suffix, seen),
	}
}

func renameGoals(gs []Term, suffix string, seen map[string]string) []Term {
	out := make([]Term, len(gs))
	for i, g := range gs {
		out[i] = renameTerm(g, suffix, seen)
	}
	return out
}

func renameTerm(t Term, suffix string, seen map[string]string) Term {
	switch x := t.(type) {
	case Var:
		nn, ok := seen[x.Name]
		if !ok {
			nn = x.Name + suffix
			seen[x.Name] = nn
		}
		return Var{nn}
	case Compound:
		args := make([]Term, len(x.Args))
		for i, a := range x.Args {
			args[i] = renameTerm(a, suffix, seen)
		}
		return Compound{Functor: x.Functor, Args: args}
	default:
		return t
	}
}

// queryVars collects the distinct variable names occurring in goals, in first-
// seen order, so [Machine.Solve] can report exactly the query's variables.
func queryVars(goals []Term) []string {
	seen := map[string]bool{}
	var out []string
	var walkT func(Term)
	walkT = func(t Term) {
		switch x := t.(type) {
		case Var:
			if !seen[x.Name] {
				seen[x.Name] = true
				out = append(out, x.Name)
			}
		case Compound:
			for _, a := range x.Args {
				walkT(a)
			}
		}
	}
	for _, g := range goals {
		walkT(g)
	}
	return out
}

// evalArith evaluates an arithmetic expression term to a kernel [arith.Num],
// reusing the ecosystem's shared numeric kernel so `is/2` matches tln core's
// arithmetic exactly. Failures are thrown as ISO error balls (instantiation_
// error, type_error(evaluable, …), evaluation_error(zero_divisor)) so they are
// catchable with catch/3.
func evalArith(t Term, s Bindings) (arith.Num, error) {
	t = Resolve(t, s)
	switch x := t.(type) {
	case Int:
		return arith.Int(x.Value), nil
	case Float:
		return arith.Float(x.Value), nil
	case Var:
		return arith.Num{}, instantiationError()
	case Atom:
		return arith.Num{}, typeError("evaluable", indicatorTerm(x.Name, 0))
	case Compound:
		switch len(x.Args) {
		case 1:
			a, err := evalArith(x.Args[0], s)
			if err != nil {
				return arith.Num{}, err
			}
			switch x.Functor {
			case "-":
				return arith.Neg(a), nil
			case "+":
				return a, nil
			case "abs":
				return arith.Abs(a), nil
			}
		case 2:
			a, err := evalArith(x.Args[0], s)
			if err != nil {
				return arith.Num{}, err
			}
			b, err := evalArith(x.Args[1], s)
			if err != nil {
				return arith.Num{}, err
			}
			res, err := arith.Binary(x.Functor, a, b)
			if err != nil {
				if errors.Is(err, arith.ErrDivByZero) || errors.Is(err, arith.ErrModByZero) {
					return arith.Num{}, evaluationError("zero_divisor")
				}
				return arith.Num{}, typeError("evaluable", indicatorTerm(x.Functor, 2))
			}
			return res, nil
		}
		return arith.Num{}, typeError("evaluable", indicatorTerm(x.Functor, len(x.Args)))
	}
	return arith.Num{}, typeError("evaluable", t)
}

// indicatorTerm builds a Name/Arity predicate-indicator term for error balls.
func indicatorTerm(name string, arity int) Term {
	return Compound{Functor: "/", Args: []Term{Atom{name}, Int{int64(arity)}}}
}

// numTerm converts a kernel [arith.Num] back to a Prolog term, preserving the
// int/float distinction.
func numTerm(n arith.Num) Term {
	if i, ok := n.Int(); ok {
		return Int{i}
	}
	return Float{n.Float()}
}

// isArithCompare reports whether functor is an arithmetic comparison operator
// (=:=, =\=, <, >, >=, =<).
func isArithCompare(functor string) bool {
	switch functor {
	case "=:=", "=\\=", "<", ">", ">=", "=<":
		return true
	}
	return false
}

// compareHolds interprets a -1/0/1 comparison under an arithmetic comparison op.
func compareHolds(op string, cmp int) bool {
	switch op {
	case "=:=":
		return cmp == 0
	case "=\\=":
		return cmp != 0
	case "<":
		return cmp < 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "=<":
		return cmp <= 0
	}
	return false
}

// SortAtoms orders a slice of ground atoms/compounds by their canonical string
// form, for deterministic output when projecting a model.
func SortAtoms(ts []Term) {
	sort.Slice(ts, func(i, j int) bool { return ts[i].String() < ts[j].String() })
}
