package prolog

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/opentalon/tln-language/pkg/arith"
)

// ---- standard order of terms ---------------------------------------------

// typeRank orders the term categories per the ISO standard order of terms:
// Var < Number < Atom < Compound.
func typeRank(t Term) int {
	switch t.(type) {
	case Var:
		return 0
	case Int, Float:
		return 1
	case Atom:
		return 2
	case Compound:
		return 3
	default:
		return 4
	}
}

// compareTerms returns -1, 0, or 1 for a, b under the standard order of terms.
// Numbers compare by value (via the shared arith kernel), ties breaking float
// before int; compounds by arity, then functor, then arguments left to right.
func compareTerms(a, b Term) int {
	ra, rb := typeRank(a), typeRank(b)
	if ra != rb {
		return sign(ra - rb)
	}
	switch x := a.(type) {
	case Var:
		return strings.Compare(x.Name, b.(Var).Name)
	case Int, Float:
		if c := arith.Compare(toNum(a), toNum(b)); c != 0 {
			return c
		}
		af, bf := isFloatTerm(a), isFloatTerm(b)
		switch {
		case af == bf:
			return 0
		case af:
			return -1
		default:
			return 1
		}
	case Atom:
		return strings.Compare(x.Name, b.(Atom).Name)
	case Compound:
		bc := b.(Compound)
		if len(x.Args) != len(bc.Args) {
			return sign(len(x.Args) - len(bc.Args))
		}
		if c := strings.Compare(x.Functor, bc.Functor); c != 0 {
			return c
		}
		for i := range x.Args {
			if c := compareTerms(x.Args[i], bc.Args[i]); c != 0 {
				return c
			}
		}
		return 0
	}
	return 0
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

func isFloatTerm(t Term) bool { _, ok := t.(Float); return ok }

// isTermCompare reports whether functor is a standard-order comparison operator.
func isTermCompare(functor string) bool {
	switch functor {
	case "==", "\\==", "@<", "@>", "@=<", "@>=":
		return true
	}
	return false
}

// termCompareHolds interprets a -1/0/1 standard-order result under a comparison
// operator.
func termCompareHolds(op string, cmp int) bool {
	switch op {
	case "==":
		return cmp == 0
	case "\\==":
		return cmp != 0
	case "@<":
		return cmp < 0
	case "@>":
		return cmp > 0
	case "@=<":
		return cmp <= 0
	case "@>=":
		return cmp >= 0
	}
	return false
}

func toNum(t Term) arith.Num {
	switch x := t.(type) {
	case Int:
		return arith.Int(x.Value)
	case Float:
		return arith.Float(x.Value)
	}
	return arith.Int(0)
}

// ---- copy_term ------------------------------------------------------------

// copyOut resolves t under s and gives every remaining free variable a fresh,
// activation-unique name, so collected solutions never share variables with the
// live goal (the basis of findall/bagof/setof and copy_term/2).
func (m *Machine) copyOut(t Term, s Bindings) Term {
	m.renameCt++
	suffix := fmt.Sprintf("~%d", m.renameCt)
	return renameTerm(Resolve(t, s), suffix, map[string]string{})
}

// ---- findall / bagof / setof ---------------------------------------------

// termVars returns the distinct variable names in t, in first-seen order.
func termVars(t Term) []string { return queryVars([]Term{t}) }

// peelCaret strips leading V^Goal existential quantifiers, returning the inner
// goal and the set of quantified variable names.
func peelCaret(g Term) (Term, map[string]bool) {
	ex := map[string]bool{}
	for {
		c, ok := g.(Compound)
		if !ok || c.Functor != "^" || len(c.Args) != 2 {
			return g, ex
		}
		for _, v := range termVars(c.Args[0]) {
			ex[v] = true
		}
		g = c.Args[1]
	}
}

// collectAll runs goal to every solution, appending copyOut(pair) for each. It
// undoes any residual bindings so callers continue from a clean substitution.
func (m *Machine) collectAll(ctx context.Context, goal Term, s Bindings, depth int, pair Term, sink func(Term)) error {
	mark := s.st.mark()
	_, _, err := m.solve(ctx, []Term{goal}, s, depth+1, func(b Bindings) bool {
		sink(m.copyOut(pair, b))
		return false // collect every solution
	})
	s.st.undoTo(mark)
	return err
}

// solveFindall implements findall/3: collect all template instances; the empty
// case yields [] rather than failing.
func (m *Machine) solveFindall(ctx context.Context, tmpl, goal, listArg Term, rest []Term, s Bindings, depth int, emit func(Bindings) bool) (bool, *bool, error) {
	var items []Term
	if err := m.collectAll(ctx, goal, s, depth, tmpl, func(t Term) { items = append(items, t) }); err != nil {
		return false, nil, err
	}
	s2, ok := Unify(listArg, List(items, Nil), s)
	if !ok {
		return false, nil, nil
	}
	return m.solve(ctx, rest, s2, depth+1, emit)
}

// solveBagof implements bagof/3 and (with sorted=true) setof/3. It groups
// solutions by the free variables of Goal (those not in Template and not ^-
// quantified) and backtracks over the groups; it fails when there are no
// solutions. setof sorts groups and each list by the standard order and removes
// duplicates.
func (m *Machine) solveBagof(ctx context.Context, tmpl, goal, listArg Term, rest []Term, s Bindings, depth int, emit func(Bindings) bool, sorted bool) (bool, *bool, error) {
	inner, ex := peelCaret(goal)

	// Free vars = vars(Goal) \ vars(Template) \ existential.
	bound := map[string]bool{}
	for _, v := range termVars(tmpl) {
		bound[v] = true
	}
	var freeTerms []Term
	for _, v := range termVars(inner) {
		if bound[v] || ex[v] {
			continue
		}
		freeTerms = append(freeTerms, Var{v})
	}
	witness := List(freeTerms, Nil)

	// Collect (witness, template) pairs, copied together so shared variables
	// stay linked.
	type sol struct{ w, item Term }
	var sols []sol
	pair := Compound{Functor: "-", Args: []Term{witness, tmpl}}
	if err := m.collectAll(ctx, inner, s, depth, pair, func(t Term) {
		pc := t.(Compound)
		sols = append(sols, sol{pc.Args[0], pc.Args[1]})
	}); err != nil {
		return false, nil, err
	}
	if len(sols) == 0 {
		return false, nil, nil // bagof/setof fail on no solutions
	}

	// Group by witness, preserving first-seen order.
	type group struct {
		w     Term
		items []Term
	}
	var groups []*group
	for _, sl := range sols {
		var g *group
		for _, cand := range groups {
			if compareTerms(cand.w, sl.w) == 0 {
				g = cand
				break
			}
		}
		if g == nil {
			g = &group{w: sl.w}
			groups = append(groups, g)
		}
		g.items = append(g.items, sl.item)
	}

	if sorted {
		sort.SliceStable(groups, func(i, j int) bool { return compareTerms(groups[i].w, groups[j].w) < 0 })
		for _, g := range groups {
			g.items = sortDedup(g.items)
		}
	}

	// Backtrack over groups: bind the free vars to each witness and the list.
	for _, g := range groups {
		s.st.undoTo(s.mark) // undo the previous group's witness bindings
		s2, ok := Unify(witness, g.w, s)
		if !ok {
			continue
		}
		s3, ok := Unify(listArg, List(g.items, Nil), s2)
		if !ok {
			continue
		}
		stop, commit, err := m.solve(ctx, rest, s3, depth+1, emit)
		if err != nil {
			return false, nil, err
		}
		if stop {
			return true, nil, nil
		}
		if commit != nil {
			return false, commit, nil
		}
	}
	return false, nil, nil
}

// sortDedup sorts terms by the standard order and drops adjacent duplicates.
func sortDedup(ts []Term) []Term {
	sort.SliceStable(ts, func(i, j int) bool { return compareTerms(ts[i], ts[j]) < 0 })
	out := ts[:0:0]
	for i, t := range ts {
		if i == 0 || compareTerms(ts[i-1], t) != 0 {
			out = append(out, t)
		}
	}
	return out
}
