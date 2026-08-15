package prolog

// Bindings is a substitution: variable name -> bound term. It is immutable under
// [Unify] — each successful binding returns a fresh map so backtracking is just
// "keep the older Bindings". This trades allocation for a trivially correct
// undo; a trail-based mutable store is the optimization a clingo-scale rewrite
// would reach for, and the [Machine] interface leaves room for it.
type Bindings map[string]Term

func (b Bindings) clone() Bindings {
	out := make(Bindings, len(b)+1)
	for k, v := range b {
		out[k] = v
	}
	return out
}

// walk follows variable bindings one level to the term a variable ultimately
// stands for, without descending into compound arguments.
func walk(t Term, s Bindings) Term {
	for {
		v, ok := t.(Var)
		if !ok {
			return t
		}
		bound, ok := s[v.Name]
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

// Unify computes the most general unifier of a and b extending s. It returns the
// extended substitution and true on success, or s and false on failure. The
// occurs-check is always on, so unification is sound (X = f(X) fails rather than
// building a cyclic term).
func Unify(a, b Term, s Bindings) (Bindings, bool) {
	a = walk(a, s)
	b = walk(b, s)

	if av, ok := a.(Var); ok {
		if bv, ok := b.(Var); ok && av.Name == bv.Name {
			return s, true
		}
		if occurs(av.Name, b, s) {
			return s, false
		}
		ns := s.clone()
		ns[av.Name] = b
		return ns, true
	}
	if bv, ok := b.(Var); ok {
		if occurs(bv.Name, a, s) {
			return s, false
		}
		ns := s.clone()
		ns[bv.Name] = a
		return ns, true
	}

	switch at := a.(type) {
	case Atom:
		bt, ok := b.(Atom)
		return s, ok && at.Name == bt.Name
	case Int:
		bt, ok := b.(Int)
		return s, ok && at.Value == bt.Value
	case Compound:
		bt, ok := b.(Compound)
		if !ok || at.Functor != bt.Functor || len(at.Args) != len(bt.Args) {
			return s, false
		}
		var ok2 bool
		for i := range at.Args {
			s, ok2 = Unify(at.Args[i], bt.Args[i], s)
			if !ok2 {
				return s, false
			}
		}
		return s, true
	}
	return s, false
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
