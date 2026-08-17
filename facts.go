package prolog

import (
	"fmt"
	"strings"

	"github.com/opentalon/tln-language/pkg/factstore"
)

// Namespace prefixes the attribute of every fact this plugin projects, so
// answers a Prolog run contributes are distinguishable in a shared FactStore
// (mirroring tln-asp's ":asp/" convention).
const Namespace = ":pl/"

// AtomFacts projects a set of ground atoms — typically the instantiated answers
// of a query, e.g. every X of ?- ancestor(tom, X) — into EAV facts a host can
// assert into any tln FactStore. The encoding mirrors tln-asp:
//
//	nullary  p        -> {RecordID: "p",        Attribute: ":pl/holds", Value: true}
//	unary    p(a)     -> {RecordID: "a",        Attribute: ":pl/p",     Value: true}
//	n-ary    p(a,…)   -> {RecordID: "p|a|…",    Attribute: ":pl/p",     Value: []any{a,…}}
//
// Crossing from structured terms back into flat EAV is lossy by choice: a
// compound or list argument has no faithful scalar form, so it is reified to its
// canonical Prolog string and a [Diagnostic] records the reification. A host that
// needs full term structure works with [Term]/[Solution] directly instead.
func AtomFacts(atoms []Term) ([]factstore.Fact, []Diagnostic) {
	out := make([]factstore.Fact, 0, len(atoms))
	var diags []Diagnostic
	for _, at := range atoms {
		name, args, ok := decompose(at)
		if !ok {
			diags = append(diags, Diagnostic{DiagUnsupported, "cannot project non-atomic term " + at.String() + " to a fact", 0})
			continue
		}
		vals := make([]any, len(args))
		for i, a := range args {
			v, lossy := scalar(a)
			vals[i] = v
			if lossy {
				diags = append(diags, Diagnostic{DiagUnsupported, "argument " + a.String() + " of " + Indicator(at) + " reified to string in fact projection", 0})
			}
		}
		switch len(args) {
		case 0:
			out = append(out, factstore.Fact{RecordID: name, Attribute: Namespace + "holds", Value: true})
		case 1:
			out = append(out, factstore.Fact{RecordID: fmt.Sprintf("%v", vals[0]), Attribute: Namespace + name, Value: true})
		default:
			out = append(out, factstore.Fact{RecordID: atomKey(name, vals), Attribute: Namespace + name, Value: vals})
		}
	}
	return out, diags
}

// decompose splits a ground atom/compound into its functor name and arguments.
func decompose(t Term) (string, []Term, bool) {
	switch x := t.(type) {
	case Atom:
		return x.Name, nil, true
	case Compound:
		return x.Functor, x.Args, true
	default:
		return "", nil, false
	}
}

// scalar converts a term argument to a fact value, reporting whether the
// conversion was lossy (a compound/list reified to its string form).
func scalar(t Term) (any, bool) {
	switch x := t.(type) {
	case Atom:
		return x.Name, false
	case Int:
		return x.Value, false
	default:
		return t.String(), true
	}
}

// atomKey builds the canonical record id for an n-ary atom: name|arg|arg|…
func atomKey(name string, args []any) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, name)
	for _, a := range args {
		parts = append(parts, fmt.Sprintf("%v", a))
	}
	return strings.Join(parts, "|")
}

// Instantiate applies a [Solution] to a template goal, returning the goal with
// its variables replaced — the bridge from "?- p(X)" plus a binding {X=a} to the
// ground atom p(a) that [AtomFacts] projects.
func Instantiate(goal Term, sol Solution) Term {
	b := NewBindings()
	for k, v := range sol {
		b.st.bind(k, v)
	}
	return Resolve(goal, b)
}
