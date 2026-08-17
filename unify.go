package prolog

// bindStore is the mutable substitution with a trail. It replaces the previous
// clone-on-write map: each new binding is recorded on the trail, and
// backtracking pops the trail back to a saved mark (O(1) bind, O(1) lookup, O(k)
// undo) instead of cloning the whole substitution on every bind.
type bindStore struct {
	m     map[string]Term
	trail []string
}

func newStore() *bindStore { return &bindStore{m: map[string]Term{}} }

func (st *bindStore) bind(name string, t Term) {
	st.m[name] = t
	st.trail = append(st.trail, name)
}

func (st *bindStore) mark() int { return len(st.trail) }

// undoTo removes every binding recorded after mark, restoring the store to the
// state it had at that mark.
func (st *bindStore) undoTo(mark int) {
	for i := len(st.trail) - 1; i >= mark; i-- {
		delete(st.m, st.trail[i])
	}
	st.trail = st.trail[:mark]
}

// Bindings is a handle into a [bindStore] at a particular trail mark. It behaves
// like an immutable substitution — extending it via [Unify] returns a new handle
// at a higher mark over the same store — as long as callers undo the store in
// stack order at choice points, which the engine does (undoTo(s.mark) before
// each alternative). Passing Bindings by value is cheap (a pointer and an int).
type Bindings struct {
	st   *bindStore
	mark int
}

// NewBindings returns an empty substitution over a fresh store.
func NewBindings() Bindings { return Bindings{st: newStore()} }

// walk follows variable bindings one level to the term a variable ultimately
// stands for, without descending into compound arguments.
func walk(t Term, s Bindings) Term {
	for {
		v, ok := t.(Var)
		if !ok {
			return t
		}
		bound, ok := s.st.m[v.Name]
		if !ok {
			return t
		}
		t = bound
	}
}

// Resolve fully instantiates a term under a substitution, replacing every bound
// variable — including those nested inside compounds — with its value.
func Resolve(t Term, s Bindings) Term {
	t = walk(t, s)
	if c, ok := t.(Compound); ok {
		args := make([]Term, len(c.Args))
		for i, a := range c.Args {
			args[i] = Resolve(a, s)
		}
		return Compound{Functor: c.Functor, Args: args}
	}
	return t
}

// Unify computes the most general unifier of a and b extending s. On success it
// returns the extended substitution and true; on failure it undoes any partial
// bindings it made and returns s and false. The occurs-check is always on, so
// unification is sound (X = f(X) fails rather than building a cyclic term).
func Unify(a, b Term, s Bindings) (Bindings, bool) {
	mark := s.st.mark()
	if unifyStep(a, b, s) {
		return Bindings{st: s.st, mark: s.st.mark()}, true
	}
	s.st.undoTo(mark)
	return s, false
}

func unifyStep(a, b Term, s Bindings) bool {
	a = walk(a, s)
	b = walk(b, s)

	if av, ok := a.(Var); ok {
		if bv, ok := b.(Var); ok && av.Name == bv.Name {
			return true
		}
		if occurs(av.Name, b, s) {
			return false
		}
		s.st.bind(av.Name, b)
		return true
	}
	if bv, ok := b.(Var); ok {
		if occurs(bv.Name, a, s) {
			return false
		}
		s.st.bind(bv.Name, a)
		return true
	}

	switch at := a.(type) {
	case Atom:
		bt, ok := b.(Atom)
		return ok && at.Name == bt.Name
	case Int:
		bt, ok := b.(Int)
		return ok && at.Value == bt.Value
	case Float:
		bt, ok := b.(Float)
		return ok && at.Value == bt.Value
	case Compound:
		bt, ok := b.(Compound)
		if !ok || at.Functor != bt.Functor || len(at.Args) != len(bt.Args) {
			return false
		}
		for i := range at.Args {
			if !unifyStep(at.Args[i], bt.Args[i], s) {
				return false
			}
		}
		return true
	}
	return false
}

// occurs reports whether variable name appears anywhere in t under s — the
// occurs-check that keeps unification sound.
func occurs(name string, t Term, s Bindings) bool {
	t = walk(t, s)
	switch x := t.(type) {
	case Var:
		return x.Name == name
	case Compound:
		for _, a := range x.Args {
			if occurs(name, a, s) {
				return true
			}
		}
	}
	return false
}
