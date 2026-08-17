// Package prolog is a self-contained logic-programming reasoner plugin for tln —
// "tln as a new Prolog front-end."
//
// tln core is a deterministic, flat EAV expert-system language: its well-founded
// resolver yields a single three-valued model over facts whose terms are only
// Var | Lit. Prolog's world is different — structured terms (compound functors,
// lists), unification with occurs-check, and SLD resolution with backtracking.
// Rather than push that into core, this plugin brings its own engine: its own
// richer term IR ([Term]), its own [Unify], its own [Machine] resolver, and its
// own ISO-subset reader ([Parse]). It is the reasoner leg of the tln plugin
// family, alongside tln-db (a store), tln-mcp (a tool), and tln-asp (a solver).
//
// The only dependency on tln-language is at the output boundary: a set of ground
// atoms projects to []factstore.Fact (see [AtomFacts]) so answers can feed any
// FactStore. Hosts that want full term structure use [Term] directly; the store
// projection is lossy by choice, exactly like tln-asp's atoms→facts.
package prolog

import (
	"fmt"
	"strconv"
	"strings"
)

// Term is a Prolog term. The concrete types are [Var], [Atom], [Int], and
// [Compound]. Lists are represented as [Compound] with functor "." and arity 2,
// terminated by the atom "[]"; the [List] and [Cons] helpers build them and
// [Term.String] renders them in list syntax.
type Term interface {
	isTerm()
	// String renders the term in canonical Prolog notation.
	String() string
}

// Var is a logic variable. Names starting with an uppercase letter or an
// underscore are variables in source; internally the engine renames them per
// clause activation to avoid capture (see [Machine]).
type Var struct{ Name string }

// Atom is a symbolic constant — a nullary functor such as foo, [], or '+'.
type Atom struct{ Name string }

// Int is an integer.
type Int struct{ Value int64 }

// Float is a floating-point number. Arithmetic (see the shared arith kernel)
// preserves the int/float distinction, so 7 and 7.0 are distinct terms.
type Float struct{ Value float64 }

// Compound is a structure functor(Args...) with arity len(Args) >= 1.
type Compound struct {
	Functor string
	Args    []Term
}

func (Var) isTerm()      {}
func (Atom) isTerm()     {}
func (Int) isTerm()      {}
func (Float) isTerm()    {}
func (Compound) isTerm() {}

func (v Var) String() string { return v.Name }
func (a Atom) String() string {
	if needsQuote(a.Name) {
		return "'" + strings.ReplaceAll(a.Name, "'", `\'`) + "'"
	}
	return a.Name
}
func (i Int) String() string   { return fmt.Sprintf("%d", i.Value) }
func (f Float) String() string { return strconv.FormatFloat(f.Value, 'g', -1, 64) }

func (c Compound) String() string { return writeTerm(c, 1200) }

// writeTerm renders a term in canonical, read-back-correct notation: known infix
// operators as `a+b` / `foo/3`, prefix operators as `\+g`, lists as [a,b|T], and
// everything else in functional `f(a,b)` form. maxPrec is the highest operator
// precedence allowed without parentheses in the current position.
func writeTerm(t Term, maxPrec int) string {
	c, ok := t.(Compound)
	if !ok {
		return t.String()
	}
	if c.Functor == "." && len(c.Args) == 2 {
		return listString(c)
	}
	if len(c.Args) == 2 {
		if info, ok := infixOps[c.Functor]; ok {
			lp, rp := info.prec, info.prec
			if info.right {
				lp-- // xfy: left binds tighter
			} else {
				rp-- // yfx: right binds tighter
			}
			s := writeTerm(c.Args[0], lp) + opText(c.Functor) + writeTerm(c.Args[1], rp)
			if info.prec > maxPrec {
				return "(" + s + ")"
			}
			return s
		}
	}
	if len(c.Args) == 1 {
		if prec, ok := prefixOps[c.Functor]; ok {
			s := c.Functor + prefixSpace(c.Functor) + writeTerm(c.Args[0], prec)
			if prec > maxPrec {
				return "(" + s + ")"
			}
			return s
		}
	}
	parts := make([]string, len(c.Args))
	for i, a := range c.Args {
		parts[i] = writeTerm(a, 999) // argument context: below the comma operator
	}
	return fmt.Sprintf("%s(%s)", Atom{c.Functor}.String(), strings.Join(parts, ","))
}

// opText renders an infix operator with surrounding layout: `,` tight, alphabetic
// operators (is, mod) spaced, symbolic operators (+, /, =) tight.
func opText(op string) string {
	if op == "," {
		return ","
	}
	if isAlphaOp(op) {
		return " " + op + " "
	}
	return op
}

// prefixSpace separates a prefix operator from its operand: a space after an
// alphabetic operator (dynamic), none after a symbolic one (\+).
func prefixSpace(op string) string {
	if isAlphaOp(op) {
		return " "
	}
	return ""
}

func isAlphaOp(op string) bool {
	for i := 0; i < len(op); i++ {
		c := op[i]
		if !(c >= 'a' && c <= 'z') {
			return false
		}
	}
	return len(op) > 0
}

// Indicator returns the term's predicate indicator name/arity — "foo/2",
// "[]/0", etc. Used by diagnostics and the facts projection.
func Indicator(t Term) string {
	switch x := t.(type) {
	case Atom:
		return x.Name + "/0"
	case Compound:
		return fmt.Sprintf("%s/%d", x.Functor, len(x.Args))
	default:
		return fmt.Sprintf("%s/0", t.String())
	}
}

// Nil is the empty-list atom "[]".
var Nil = Atom{"[]"}

// Cons builds a list cell [Head | Tail].
func Cons(head, tail Term) Term { return Compound{Functor: ".", Args: []Term{head, tail}} }

// List builds a proper or partial list [items... | tail]. Pass Nil as tail for
// a proper list.
func List(items []Term, tail Term) Term {
	out := tail
	for i := len(items) - 1; i >= 0; i-- {
		out = Cons(items[i], out)
	}
	return out
}

// listString renders a "."/2 chain in [a,b|T] notation.
func listString(c Compound) string {
	var b strings.Builder
	b.WriteByte('[')
	var cur Term = c
	first := true
	for {
		cell, ok := cur.(Compound)
		if !ok || cell.Functor != "." || len(cell.Args) != 2 {
			break
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(cell.Args[0].String())
		cur = cell.Args[1]
	}
	if a, ok := cur.(Atom); !ok || a.Name != "[]" {
		b.WriteByte('|')
		b.WriteString(cur.String())
	}
	b.WriteByte(']')
	return b.String()
}

// needsQuote reports whether an atom name must be single-quoted to read back.
func needsQuote(name string) bool {
	if name == "" {
		return true
	}
	if name == "[]" || name == "!" || name == ";" {
		return false
	}
	// Symbolic atoms (e.g. <, =, @<, \==, ->) read back unquoted.
	if isSymbolicAtom(name) {
		return false
	}
	if name[0] < 'a' || name[0] > 'z' {
		return true
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return true
		}
	}
	return false
}

// isSymbolicAtom reports whether name is a non-empty run of ISO symbol
// characters — such atoms (operators like <, =, @<, ->) print without quotes.
func isSymbolicAtom(name string) bool {
	const symbolChars = "+-*/\\^<>=~:.?@#&$"
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if strings.IndexByte(symbolChars, name[i]) < 0 {
			return false
		}
	}
	return true
}
