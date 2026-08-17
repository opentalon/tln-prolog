package prolog

// Run-scoped clause-database mutation (Phase 5b). assert/retract operate on a
// per-run copy of the clause database (m.runClauses) that is discarded when the
// query ends, so changes never outlive the run. Only predicates declared
// `:- dynamic` may be modified; anything else throws a permission_error.

// clauseTerm renders a clause as the term the user would assert/retract:
// a bare Head for a fact, or Head :- Body for a rule.
func clauseTerm(c Clause) Term {
	if len(c.Body) == 0 {
		return c.Head
	}
	return Compound{Functor: ":-", Args: []Term{c.Head, conjoin(c.Body)}}
}

// conjoin rebuilds a goal slice into a right-nested ","/2 conjunction.
func conjoin(goals []Term) Term {
	if len(goals) == 1 {
		return goals[0]
	}
	return Compound{Functor: ",", Args: []Term{goals[0], conjoin(goals[1:])}}
}

// permissionError is the ISO permission_error(Op, Type, Culprit) ball.
func permissionError(op, typ string, culprit Term) error {
	return errorBall(Compound{Functor: "permission_error", Args: []Term{Atom{op}, Atom{typ}, culprit}})
}

// headIndicator returns the "name/arity" of a clause head, and the indicator
// term for error balls.
func headIndicator(head Term) (string, Term) {
	switch h := head.(type) {
	case Compound:
		return Indicator(h), indicatorTerm(h.Functor, len(h.Args))
	case Atom:
		return h.Name + "/0", indicatorTerm(h.Name, 0)
	}
	return "", Atom{"?"}
}

// assert adds a clause to the run store. asserta prepends, assertz/assert
// append. The clause is copied (fresh variables) so it is independent of the
// live goal's bindings. Each mutation creates a new slice so any clause loop
// already in progress keeps the logical (pre-mutation) view.
func (m *Machine) assert(clauseArg Term, s Bindings, prepend bool) error {
	cp := m.copyOut(clauseArg, s)
	head, body := splitClause(cp)
	pi, piTerm := headIndicator(head)
	if !m.dynamic[pi] {
		return permissionError("modify", "static_procedure", piTerm)
	}
	nc := Clause{Head: head, Body: body}
	if prepend {
		m.runClauses = append([]Clause{nc}, m.runClauses...)
		m.mutations = append(m.mutations, Mutation{Op: "asserta", Clause: clauseTerm(nc).String()})
	} else {
		m.runClauses = append(append([]Clause{}, m.runClauses...), nc)
		m.mutations = append(m.mutations, Mutation{Op: "assertz", Clause: clauseTerm(nc).String()})
	}
	return nil
}

// retract removes the first run-store clause that unifies with the pattern and
// returns the resulting bindings. It is semi-deterministic (first match only);
// backtracking does not remove further clauses.
func (m *Machine) retract(clauseArg Term, s Bindings) (Bindings, bool, error) {
	pattern := Resolve(clauseArg, s)
	head, _ := splitClause(pattern)
	pi, piTerm := headIndicator(head)
	if !m.dynamic[pi] {
		return s, false, permissionError("modify", "static_procedure", piTerm)
	}
	for i, c := range m.runClauses {
		s.st.undoTo(s.mark)
		rc := m.rename(c)
		if s2, ok := Unify(pattern, clauseTerm(rc), s); ok {
			// Remove clause i, building a new slice so in-progress loops are
			// unaffected (logical update view).
			nn := make([]Clause, 0, len(m.runClauses)-1)
			nn = append(nn, m.runClauses[:i]...)
			nn = append(nn, m.runClauses[i+1:]...)
			m.runClauses = nn
			m.mutations = append(m.mutations, Mutation{Op: "retract", Clause: clauseTerm(rc).String()})
			return s2, true, nil
		}
	}
	return s, false, nil
}
