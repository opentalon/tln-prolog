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

// NewMachine builds a resolver over clauses.
func NewMachine(clauses []Clause, opts ...Option) *Machine {
	m := &Machine{clauses: clauses, maxDepth: 4096}
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

	stop, err := m.solve(ctx, goals, Bindings{}, 0, func(s Bindings) bool {
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
	_ = stop
	return out, nil
}

// Prove reports whether the goal conjunction has at least one solution.
func (m *Machine) Prove(ctx context.Context, goals []Term) (bool, error) {
	sols, err := m.Solve(ctx, goals, 1)
	return len(sols) > 0, err
}

// solve is the recursive SLD engine. emit is called with each complete
// substitution and returns true to stop the search early. The bool result
// propagates that stop signal up the stack.
func (m *Machine) solve(ctx context.Context, goals []Term, s Bindings, depth int, emit func(Bindings) bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(goals) == 0 {
		return emit(s), nil
	}
	if depth > m.maxDepth {
		return false, ErrDepthExceeded
	}

	goal := walk(goals[0], s)
	rest := goals[1:]

	// Built-in control and unification. Everything Prolog-ish that this engine
	// does NOT execute (cut, IO, assert/retract, arithmetic) is deliberately
	// absent here and flagged by the reader's diagnostics instead.
	switch g := goal.(type) {
	case Atom:
		switch g.Name {
		case "true":
			return m.solve(ctx, rest, s, depth+1, emit)
		case "fail", "false":
			return false, nil
		}
	case Compound:
		switch {
		case g.Functor == "," && len(g.Args) == 2:
			return m.solve(ctx, append([]Term{g.Args[0], g.Args[1]}, rest...), s, depth+1, emit)
		case g.Functor == "=" && len(g.Args) == 2:
			s2, ok := Unify(g.Args[0], g.Args[1], s)
			if !ok {
				return false, nil
			}
			return m.solve(ctx, rest, s2, depth+1, emit)
		case g.Functor == "\\=" && len(g.Args) == 2:
			if _, ok := Unify(g.Args[0], g.Args[1], s); ok {
				return false, nil
			}
			return m.solve(ctx, rest, s, depth+1, emit)
		case g.Functor == "is" && len(g.Args) == 2:
			v, err := evalArith(g.Args[1], s)
			if err != nil {
				return false, err
			}
			s2, ok := Unify(g.Args[0], numTerm(v), s)
			if !ok {
				return false, nil
			}
			return m.solve(ctx, rest, s2, depth+1, emit)
		case isArithCompare(g.Functor) && len(g.Args) == 2:
			a, err := evalArith(g.Args[0], s)
			if err != nil {
				return false, err
			}
			b, err := evalArith(g.Args[1], s)
			if err != nil {
				return false, err
			}
			if !compareHolds(g.Functor, arith.Compare(a, b)) {
				return false, nil
			}
			return m.solve(ctx, rest, s, depth+1, emit)
		}
	}

	// User clauses: try each, renamed fresh so its variables never clash.
	for _, c := range m.clauses {
		rc := m.rename(c)
		s2, ok := Unify(goal, rc.Head, s)
		if !ok {
			continue
		}
		next := append(append([]Term{}, rc.Body...), rest...)
		stop, err := m.solve(ctx, next, s2, depth+1, emit)
		if err != nil {
			return false, err
		}
		if stop {
			return true, nil
		}
	}
	return false, nil
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
// arithmetic exactly. Unbound variables and non-evaluable terms are errors —
// ISO Prolog would throw; the engine surfaces them through the error channel
// until throw/catch lands (Phase 5a).
func evalArith(t Term, s Bindings) (arith.Num, error) {
	t = Resolve(t, s)
	switch x := t.(type) {
	case Int:
		return arith.Int(x.Value), nil
	case Float:
		return arith.Float(x.Value), nil
	case Var:
		return arith.Num{}, fmt.Errorf("prolog: arithmetic on unbound variable %s", x.Name)
	case Atom:
		return arith.Num{}, fmt.Errorf("prolog: %s is not an evaluable constant", x.String())
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
			return arith.Binary(x.Functor, a, b)
		}
		return arith.Num{}, fmt.Errorf("prolog: %s is not an arithmetic function", Indicator(x))
	}
	return arith.Num{}, fmt.Errorf("prolog: cannot evaluate %s", t.String())
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
