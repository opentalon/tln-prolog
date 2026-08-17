package prolog

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file holds the standard term/list/atom builtins (Phase 4): structure
// inspection (functor/3, arg/3, =../2), atom<->codes/chars conversions, the
// type-test family, and Go-side sort/2 & msort/2. Pure list predicates
// (append/3, member/2, …) live in the bootstrap prelude, see prelude.go.

// termToList walks a "."/2 chain to a slice; ok is false for a partial or
// non-list term.
func termToList(t Term, s Bindings) ([]Term, bool) {
	var out []Term
	cur := walk(t, s)
	for {
		if a, ok := cur.(Atom); ok && a.Name == "[]" {
			return out, true
		}
		c, ok := cur.(Compound)
		if !ok || c.Functor != "." || len(c.Args) != 2 {
			return nil, false
		}
		out = append(out, c.Args[0])
		cur = walk(c.Args[1], s)
	}
}

// atomicText renders an atom or number to its text form.
func atomicText(t Term) (string, bool) {
	switch x := t.(type) {
	case Atom:
		return x.Name, true
	case Int, Float:
		return x.String(), true
	}
	return "", false
}

// parseNumber parses text as an Int, else a Float; nil if neither.
func parseNumber(text string) Term {
	if i, err := strconv.ParseInt(text, 10, 64); err == nil {
		return Int{i}
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return Float{f}
	}
	return nil
}

// biFunctor implements functor/3 in both directions.
func (m *Machine) biFunctor(t, name, arity Term, s Bindings) (Bindings, bool) {
	tv := walk(t, s)
	if _, isVar := tv.(Var); !isVar {
		var nm Term
		ar := 0
		switch x := tv.(type) {
		case Compound:
			nm, ar = Atom{x.Functor}, len(x.Args)
		case Atom, Int, Float:
			nm = tv
		default:
			return s, false
		}
		s2, ok := Unify(name, nm, s)
		if !ok {
			return s, false
		}
		return Unify(arity, Int{int64(ar)}, s2)
	}
	// Construct a term from name + arity.
	ai, ok := walk(arity, s).(Int)
	if !ok {
		return s, false
	}
	nmv := walk(name, s)
	if ai.Value == 0 {
		return Unify(t, nmv, s)
	}
	fa, ok := nmv.(Atom)
	if !ok {
		return s, false
	}
	args := make([]Term, ai.Value)
	for i := range args {
		m.renameCt++
		args[i] = Var{fmt.Sprintf("_G%d", m.renameCt)}
	}
	return Unify(t, Compound{Functor: fa.Name, Args: args}, s)
}

// biArg implements arg/3: the N-th (1-based) argument of a compound.
func biArg(n, cmp, arg Term, s Bindings) (Bindings, bool) {
	nv, ok := walk(n, s).(Int)
	if !ok {
		return s, false
	}
	cv, ok := walk(cmp, s).(Compound)
	if !ok || nv.Value < 1 || int(nv.Value) > len(cv.Args) {
		return s, false
	}
	return Unify(arg, cv.Args[nv.Value-1], s)
}

// biUniv implements =../2 (univ) in both directions.
func (m *Machine) biUniv(t, list Term, s Bindings) (Bindings, bool) {
	tv := walk(t, s)
	if _, isVar := tv.(Var); !isVar {
		var elems []Term
		if c, ok := tv.(Compound); ok {
			elems = append([]Term{Atom{c.Functor}}, c.Args...)
		} else {
			elems = []Term{tv}
		}
		return Unify(list, List(elems, Nil), s)
	}
	elems, ok := termToList(list, s)
	if !ok || len(elems) == 0 {
		return s, false
	}
	head := walk(elems[0], s)
	if len(elems) == 1 {
		return Unify(t, head, s)
	}
	fa, ok := head.(Atom)
	if !ok {
		return s, false
	}
	return Unify(t, Compound{Functor: fa.Name, Args: elems[1:]}, s)
}

// biAtomText implements atom_codes/2 and atom_chars/2 (asChars) in both
// directions, and number_codes/2 (asNumber, parsing the built text).
func (m *Machine) biAtomText(a, seq Term, s Bindings, asChars, asNumber bool) (Bindings, bool) {
	av := walk(a, s)
	if _, isVar := av.(Var); !isVar {
		text, ok := atomicText(av)
		if !ok {
			return s, false
		}
		var elems []Term
		for _, r := range text {
			if asChars {
				elems = append(elems, Atom{string(r)})
			} else {
				elems = append(elems, Int{int64(r)})
			}
		}
		return Unify(seq, List(elems, Nil), s)
	}
	elems, ok := termToList(seq, s)
	if !ok {
		return s, false
	}
	var b strings.Builder
	for _, e := range elems {
		switch ev := walk(e, s).(type) {
		case Atom:
			if !asChars {
				return s, false
			}
			b.WriteString(ev.Name)
		case Int:
			if asChars {
				return s, false
			}
			b.WriteRune(rune(ev.Value))
		default:
			return s, false
		}
	}
	if asNumber {
		nt := parseNumber(b.String())
		if nt == nil {
			return s, false
		}
		return Unify(a, nt, s)
	}
	return Unify(a, Atom{b.String()}, s)
}

// biCharCode implements char_code/2 in both directions.
func biCharCode(ch, code Term, s Bindings) (Bindings, bool) {
	if ca, ok := walk(ch, s).(Atom); ok {
		r := []rune(ca.Name)
		if len(r) != 1 {
			return s, false
		}
		return Unify(code, Int{int64(r[0])}, s)
	}
	ci, ok := walk(code, s).(Int)
	if !ok {
		return s, false
	}
	return Unify(ch, Atom{string(rune(ci.Value))}, s)
}

// biAtomLength implements atom_length/2.
func biAtomLength(a, l Term, s Bindings) (Bindings, bool) {
	text, ok := atomicText(walk(a, s))
	if !ok {
		return s, false
	}
	return Unify(l, Int{int64(len([]rune(text)))}, s)
}

// biSort implements msort/2 (dedup=false) and sort/2 (dedup=true).
func biSort(list, out Term, s Bindings, dedup bool) (Bindings, bool) {
	elems, ok := termToList(list, s)
	if !ok {
		return s, false
	}
	cp := make([]Term, len(elems))
	for i, e := range elems {
		cp[i] = Resolve(e, s)
	}
	if dedup {
		cp = sortDedup(cp)
	} else {
		sort.SliceStable(cp, func(i, j int) bool { return compareTerms(cp[i], cp[j]) < 0 })
	}
	return Unify(out, List(cp, Nil), s)
}

// isTypeTest reports whether functor is a 1-arg type-checking predicate.
func isTypeTest(functor string) bool {
	switch functor {
	case "var", "nonvar", "atom", "atomic", "number", "integer", "float",
		"compound", "callable", "is_list", "ground":
		return true
	}
	return false
}

// typeTestHolds evaluates a type-test predicate on its (resolved) argument.
func typeTestHolds(name string, arg Term, s Bindings) bool {
	t := walk(arg, s)
	switch name {
	case "var":
		_, ok := t.(Var)
		return ok
	case "nonvar":
		_, ok := t.(Var)
		return !ok
	case "atom":
		_, ok := t.(Atom)
		return ok
	case "integer":
		_, ok := t.(Int)
		return ok
	case "float":
		_, ok := t.(Float)
		return ok
	case "number":
		switch t.(type) {
		case Int, Float:
			return true
		}
		return false
	case "atomic":
		switch t.(type) {
		case Atom, Int, Float:
			return true
		}
		return false
	case "compound":
		_, ok := t.(Compound)
		return ok
	case "callable":
		switch t.(type) {
		case Atom, Compound:
			return true
		}
		return false
	case "is_list":
		_, ok := termToList(t, s)
		return ok
	case "ground":
		return isGround(Resolve(t, s))
	}
	return false
}

// isGround reports whether t contains no unbound variables.
func isGround(t Term) bool {
	switch x := t.(type) {
	case Var:
		return false
	case Compound:
		for _, a := range x.Args {
			if !isGround(a) {
				return false
			}
		}
	}
	return true
}
