package prolog

import (
	"context"
	"errors"
	"sort"
)

// Tabling under well-founded semantics (Phase 6).
//
// Tabled predicates are evaluated by grounding their clauses and computing the
// three-valued well-founded model with Van Gelder's alternating fixpoint,
// instead of by SLD resolution (which loops on left recursion and gives the
// wrong answer for non-stratified negation). The query then reads the model:
// true atoms succeed, and \+a succeeds only when a is *false* — an undefined
// atom is neither, so a negative loop like win(X):-move(X,Y),\+win(Y) over a
// 2-cycle correctly yields "undefined" (a drawn game) rather than looping.
//
// The grounding is naive (recompute the least fixpoint each step) and assumes a
// finite grounding — i.e. range-restricted, Datalog-shaped tabled rules. Rules
// that build unbounded terms will hit the tabling budget.

// ErrTablingBudget is returned when the well-founded fixpoint does not converge
// within the iteration budget (typically an unbounded grounding).
var ErrTablingBudget = errors.New("prolog: tabling fixpoint budget exceeded")

const tablingIterCap = 1 << 20

// wfsModel is the three-valued well-founded model over ground tabled atoms.
type wfsModel struct {
	trueAtoms map[string]Term // key -> ground atom, definitely true
	falseKeys map[string]bool // definitely false
	undefAtom map[string]Term // undefined (in the well-founded sense)
}

// computeWFS builds the well-founded model of the tabled predicates.
func (m *Machine) computeWFS(ctx context.Context) (*wfsModel, error) {
	m.tabledClauses = m.tabledClauses[:0]
	for _, c := range m.clauses {
		if m.tabled[Indicator(c.Head)] {
			m.tabledClauses = append(m.tabledClauses, c)
		}
	}
	m.wfsUniverse = map[string]Term{}

	// true = lfp(Φ²): the alternating fixpoint's lower bound.
	trueSet := map[string]Term{}
	for i := 0; ; i++ {
		if i > tablingIterCap {
			return nil, ErrTablingBudget
		}
		p1, err := m.phi(ctx, keySet(trueSet))
		if err != nil {
			return nil, err
		}
		p2, err := m.phi(ctx, keySet(p1))
		if err != nil {
			return nil, err
		}
		if sameKeys(p2, trueSet) {
			break
		}
		trueSet = p2
	}
	// U = Φ(true): the upper bound (possibly true).
	upper, err := m.phi(ctx, keySet(trueSet))
	if err != nil {
		return nil, err
	}

	model := &wfsModel{
		trueAtoms: trueSet,
		falseKeys: map[string]bool{},
		undefAtom: map[string]Term{},
	}
	for k, a := range m.wfsUniverse {
		if _, ok := trueSet[k]; ok {
			continue // true
		}
		if _, ok := upper[k]; ok {
			model.undefAtom[k] = a // possibly-true but not definitely: undefined
		} else {
			model.falseKeys[k] = true
		}
	}
	return model, nil
}

// phi computes Φ(J): the least fixpoint of positive immediate consequence where
// a negative literal \+a is satisfied iff a ∉ J.
func (m *Machine) phi(ctx context.Context, j map[string]bool) (map[string]Term, error) {
	i := map[string]Term{}
	for iter := 0; ; iter++ {
		if iter > tablingIterCap {
			return nil, ErrTablingBudget
		}
		next, err := m.tpStep(ctx, i, j)
		if err != nil {
			return nil, err
		}
		if len(next) == len(i) { // monotone growth: stable size ⇒ fixpoint
			return next, nil
		}
		i = next
	}
}

// tpStep applies one round of immediate consequence: for each tabled clause,
// derive every ground head whose body holds given positive tabled deps in i and
// negative tabled deps judged against j.
func (m *Machine) tpStep(ctx context.Context, i map[string]Term, j map[string]bool) (map[string]Term, error) {
	out := cloneAtoms(i)

	prevMode, prevOracle, prevJ := m.tabMode, m.posOracle, m.negJ
	m.tabMode, m.posOracle, m.negJ = tabGrounding, i, j
	defer func() { m.tabMode, m.posOracle, m.negJ = prevMode, prevOracle, prevJ }()

	for _, c := range m.tabledClauses {
		rc := m.rename(c)
		_, _, err := m.solve(ctx, rc.Body, Bindings{}, 0, func(s Bindings) bool {
			h := Resolve(rc.Head, s)
			if isGround(h) {
				k := h.String()
				out[k] = h
				m.wfsUniverse[k] = h
			}
			return false // collect every derivation
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// answerTabled enumerates the tabled goal's true instances from the current
// positive oracle (the fixpoint's i during grounding, the model's true atoms at
// query time), in a deterministic order.
func (m *Machine) answerTabled(ctx context.Context, goal Term, rest []Term, s Bindings, depth int, emit func(Bindings) bool) (bool, *bool, error) {
	for _, a := range sortedAtoms(m.posOracle) {
		s2, ok := Unify(goal, a, s)
		if !ok {
			continue
		}
		stop, commit, err := m.solve(ctx, rest, s2, depth+1, emit)
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

// negHolds reports whether \+a holds for a ground tabled atom a: during
// grounding, iff a ∉ J (and a is recorded in the universe); at query time, iff
// a is in the model's false set.
func (m *Machine) negHolds(a Term) bool {
	k := a.String()
	if m.tabMode == tabGrounding {
		if m.wfsUniverse != nil {
			m.wfsUniverse[k] = a
		}
		return !m.negJ[k]
	}
	return m.negFalse[k]
}

// isTabledGoal reports whether goal names a tabled predicate.
func (m *Machine) isTabledGoal(goal Term) bool {
	switch goal.(type) {
	case Compound, Atom:
		return m.tabled[Indicator(goal)]
	}
	return false
}

// WFSResult is the well-founded model as sorted lists of canonical atom strings.
type WFSResult struct {
	True      []string
	False     []string
	Undefined []string
}

// WellFounded returns the well-founded model computed by the most recent Solve
// over a program with tabled predicates (empty if none).
func (m *Machine) WellFounded() WFSResult {
	var r WFSResult
	if m.lastModel == nil {
		return r
	}
	for k := range m.lastModel.trueAtoms {
		r.True = append(r.True, k)
	}
	for k := range m.lastModel.falseKeys {
		r.False = append(r.False, k)
	}
	for k := range m.lastModel.undefAtom {
		r.Undefined = append(r.Undefined, k)
	}
	sort.Strings(r.True)
	sort.Strings(r.False)
	sort.Strings(r.Undefined)
	return r
}

// ---- small set helpers ----------------------------------------------------

func keySet(atoms map[string]Term) map[string]bool {
	out := make(map[string]bool, len(atoms))
	for k := range atoms {
		out[k] = true
	}
	return out
}

func cloneAtoms(atoms map[string]Term) map[string]Term {
	out := make(map[string]Term, len(atoms))
	for k, v := range atoms {
		out[k] = v
	}
	return out
}

func sameKeys(a, b map[string]Term) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func sortedAtoms(atoms map[string]Term) []Term {
	keys := make([]string, 0, len(atoms))
	for k := range atoms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Term, len(keys))
	for i, k := range keys {
		out[i] = atoms[k]
	}
	return out
}
