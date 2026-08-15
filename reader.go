package prolog

import (
	"fmt"
	"strconv"
	"strings"
)

// DiagnosticKind classifies a reader diagnostic.
type DiagnosticKind string

const (
	// DiagCut marks a cut (!) — control this engine's SLD resolver does not
	// implement; it is parsed as the atom '!' and ignored during solving.
	DiagCut DiagnosticKind = "cut"
	// DiagIO marks a side-effecting IO predicate (write/1, nl/0, read/1, …).
	DiagIO DiagnosticKind = "io"
	// DiagDatabase marks database mutation (assert/1, asserta, assertz, retract).
	DiagDatabase DiagnosticKind = "database"
	// DiagArith marks arithmetic / comparison (is/2, </2, >/2, =:=/2, …) which
	// the engine does not evaluate.
	DiagArith DiagnosticKind = "arith"
	// DiagUnsupported marks any other construct the reader recognises but the
	// engine cannot run (findall/3, float literals, etc.).
	DiagUnsupported DiagnosticKind = "unsupported"
	// DiagSyntax marks a parse error; the offending clause is skipped.
	DiagSyntax DiagnosticKind = "syntax"
)

// Diagnostic reports a construct the reader accepted syntactically but the
// engine does not yet execute — the honest boundary of the Prolog it runs. The
// porting pipeline reads these to decide, per clause, what lowers to native tln
// rules and what stays on this engine.
type Diagnostic struct {
	Kind    DiagnosticKind
	Message string
	Line    int
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("line %d: %s: %s", d.Line, d.Kind, d.Message)
}

// Program is the result of reading Prolog source: the clauses the engine can run
// plus the diagnostics for everything it cannot.
type Program struct {
	Clauses     []Clause
	Diagnostics []Diagnostic
}

// Parse reads Prolog source into a [Program]. It never returns an error: syntax
// problems become [DiagSyntax] diagnostics on the offending clause so a whole
// file still yields its runnable clauses. Supported: facts and rules (:-),
// conjunction (,), atoms, integers, variables, compound terms, lists
// ([a,b|T]), parenthesised terms, and a small operator table (=, \=, is, the
// comparison operators, + - * /). Unsupported constructs are recorded as
// diagnostics, not silently dropped.
func Parse(src string) *Program {
	prog := &Program{}
	p := &parser{lex: newLexer(src)}
	for {
		clause, ok, diags := p.clause()
		prog.Diagnostics = append(prog.Diagnostics, diags...)
		if !ok {
			break
		}
		if clause != nil {
			prog.Clauses = append(prog.Clauses, *clause)
		}
	}
	return prog
}

// ParseTerm reads a single term (e.g. a query goal) from src. The trailing "."
// is optional.
func ParseTerm(src string) (Term, []Diagnostic, error) {
	s := strings.TrimSpace(src)
	s = strings.TrimSuffix(s, ".")
	p := &parser{lex: newLexer(s + " .")}
	t, diags, err := p.term(1200)
	return t, diags, err
}

// ---- lexer ---------------------------------------------------------------

type tokKind int

const (
	tEOF tokKind = iota
	tAtom
	tVar
	tInt
	tPunct // ( ) [ ] , | and the clause terminator .
	tOp    // :- = \= is < > =< >= == + - * / etc.
	tFloat // recognised only to diagnose it
)

type token struct {
	kind tokKind
	text string
	line int
}

type lexer struct {
	src  string
	pos  int
	line int
}

func newLexer(src string) *lexer { return &lexer{src: src, line: 1} }

const symbolChars = "+-*/\\^<>=~:.?@#&"

func (l *lexer) next() token {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n':
			l.line++
			l.pos++
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case c == '%': // line comment
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*': // block comment
			l.pos += 2
			for l.pos+1 < len(l.src) && !(l.src[l.pos] == '*' && l.src[l.pos+1] == '/') {
				if l.src[l.pos] == '\n' {
					l.line++
				}
				l.pos++
			}
			l.pos += 2
		default:
			return l.lexToken()
		}
	}
	return token{kind: tEOF, line: l.line}
}

func (l *lexer) lexToken() token {
	start := l.pos
	c := l.src[l.pos]
	ln := l.line

	switch {
	case c == '(' || c == ')' || c == '[' || c == ']' || c == ',' || c == '|' || c == '{' || c == '}':
		l.pos++
		return token{tPunct, string(c), ln}
	case c == '!':
		l.pos++
		return token{tAtom, "!", ln}
	case c == ';':
		l.pos++
		return token{tAtom, ";", ln}
	case c == '\'': // quoted atom
		l.pos++
		var b strings.Builder
		for l.pos < len(l.src) && l.src[l.pos] != '\'' {
			if l.src[l.pos] == '\\' && l.pos+1 < len(l.src) {
				l.pos++
			}
			b.WriteByte(l.src[l.pos])
			l.pos++
		}
		l.pos++ // closing quote
		return token{tAtom, b.String(), ln}
	case c >= '0' && c <= '9':
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
		if l.pos < len(l.src) && l.src[l.pos] == '.' && l.pos+1 < len(l.src) &&
			l.src[l.pos+1] >= '0' && l.src[l.pos+1] <= '9' {
			l.pos++
			for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
				l.pos++
			}
			return token{tFloat, l.src[start:l.pos], ln}
		}
		return token{tInt, l.src[start:l.pos], ln}
	case c == '_' || (c >= 'A' && c <= 'Z'):
		for l.pos < len(l.src) && isIdent(l.src[l.pos]) {
			l.pos++
		}
		return token{tVar, l.src[start:l.pos], ln}
	case c >= 'a' && c <= 'z':
		for l.pos < len(l.src) && isIdent(l.src[l.pos]) {
			l.pos++
		}
		return token{tAtom, l.src[start:l.pos], ln}
	case strings.IndexByte(symbolChars, c) >= 0:
		for l.pos < len(l.src) && strings.IndexByte(symbolChars, l.src[l.pos]) >= 0 {
			l.pos++
		}
		sym := l.src[start:l.pos]
		// A lone "." at clause end is the terminator, not an operator.
		if sym == "." {
			return token{tPunct, ".", ln}
		}
		return token{tOp, sym, ln}
	default:
		l.pos++
		return token{tOp, string(c), ln}
	}
}

func isIdent(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// ---- parser --------------------------------------------------------------

// operator precedence table (subset of ISO). Lower binds tighter.
type opInfo struct {
	prec  int
	right bool // right-associative (xfy) vs left (yfx/xfx treated as left)
}

var infixOps = map[string]opInfo{
	":-": {1200, true},
	";":  {1100, true},
	"->": {1050, true},
	",":  {1000, true},
	"=":  {700, false}, "\\=": {700, false},
	"==": {700, false}, "\\==": {700, false},
	"is": {700, false}, "=:=": {700, false}, "=\\=": {700, false},
	"<": {700, false}, ">": {700, false}, "=<": {700, false}, ">=": {700, false},
	"@<": {700, false}, "@>": {700, false}, "@=<": {700, false}, "@>=": {700, false},
	"+": {500, false}, "-": {500, false},
	"*": {400, false}, "/": {400, false}, "//": {400, false}, "mod": {400, false},
}

type parser struct {
	lex  *lexer
	peek *token
}

func (p *parser) advance() token {
	if p.peek != nil {
		t := *p.peek
		p.peek = nil
		return t
	}
	return p.lex.next()
}

// unread pushes a token back so the next advance() returns it. Used on the
// error path so a clause terminator "." consumed while scanning arguments is
// handed to resync instead of being swallowed (which would eat the next,
// well-formed clause).
func (p *parser) unread(t token) { p.peek = &t }

func (p *parser) peekTok() token {
	if p.peek == nil {
		t := p.lex.next()
		p.peek = &t
	}
	return *p.peek
}

// clause reads one clause terminated by ".". ok=false signals EOF. On a syntax
// error it returns a nil clause plus a diagnostic and resynchronises past the
// next ".".
func (p *parser) clause() (*Clause, bool, []Diagnostic) {
	if p.peekTok().kind == tEOF {
		return nil, false, nil
	}
	t, diags, err := p.term(1200)
	if err != nil {
		diags = append(diags, Diagnostic{DiagSyntax, err.Error(), p.peekTok().line})
		p.resync()
		return nil, true, diags
	}
	// consume terminating "."
	end := p.advance()
	if !(end.kind == tPunct && end.text == ".") {
		diags = append(diags, Diagnostic{DiagSyntax, "expected '.' at end of clause", end.line})
		p.resync()
		return nil, true, diags
	}

	head, body := splitClause(t)
	diags = append(diags, scanDiagnostics(body, end.line)...)
	return &Clause{Head: head, Body: body}, true, diags
}

func (p *parser) resync() {
	for {
		t := p.advance()
		if t.kind == tEOF || (t.kind == tPunct && t.text == ".") {
			return
		}
	}
}

// term is a precedence-climbing parser up to maxPrec.
func (p *parser) term(maxPrec int) (Term, []Diagnostic, error) {
	left, diags, err := p.primary()
	if err != nil {
		return nil, diags, err
	}
	for {
		tok := p.peekTok()
		var opName string
		switch {
		case tok.kind == tOp:
			opName = tok.text
		case tok.kind == tPunct && tok.text == ",":
			opName = ","
		case tok.kind == tPunct && tok.text == "|":
			opName = "|" // only meaningful inside lists; stop here otherwise
		case tok.kind == tAtom && tok.text == ";":
			opName = ";"
		case tok.kind == tAtom && (tok.text == "is" || tok.text == "mod"):
			opName = tok.text
		}
		info, ok := infixOps[opName]
		if !ok || info.prec > maxPrec {
			break
		}
		p.advance() // consume operator
		nextMax := info.prec
		if !info.right {
			nextMax = info.prec - 1
		}
		right, d2, err := p.term(nextMax)
		diags = append(diags, d2...)
		if err != nil {
			return nil, diags, err
		}
		left = Compound{Functor: opName, Args: []Term{left, right}}
	}
	return left, diags, nil
}

func (p *parser) primary() (Term, []Diagnostic, error) {
	tok := p.advance()
	switch tok.kind {
	case tInt:
		n, _ := strconv.ParseInt(tok.text, 10, 64)
		return Int{n}, nil, nil
	case tFloat:
		return Atom{tok.text}, []Diagnostic{{DiagUnsupported, "float literal " + tok.text + " read as atom (engine is integer-only)", tok.line}}, nil
	case tVar:
		return Var{tok.text}, nil, nil
	case tOp:
		// prefix minus on a number: -3
		if tok.text == "-" {
			nt := p.peekTok()
			if nt.kind == tInt {
				p.advance()
				n, _ := strconv.ParseInt(nt.text, 10, 64)
				return Int{-n}, nil, nil
			}
		}
		return nil, nil, fmt.Errorf("unexpected operator %q", tok.text)
	case tAtom:
		// compound?  atom '(' args ')'
		if p.peekTok().kind == tPunct && p.peekTok().text == "(" {
			p.advance() // (
			args, diags, err := p.argList(")")
			if err != nil {
				return nil, diags, err
			}
			return Compound{Functor: tok.text, Args: args}, diags, nil
		}
		return Atom{tok.text}, nil, nil
	case tPunct:
		switch tok.text {
		case "(":
			inner, diags, err := p.term(1200)
			if err != nil {
				return nil, diags, err
			}
			closeTok := p.advance()
			if !(closeTok.kind == tPunct && closeTok.text == ")") {
				return nil, diags, fmt.Errorf("expected ')'")
			}
			return inner, diags, nil
		case "[":
			return p.list()
		}
	}
	return nil, nil, fmt.Errorf("unexpected token %q", tok.text)
}

// argList parses comma-separated arguments up to the closing punct (") ").
func (p *parser) argList(closing string) ([]Term, []Diagnostic, error) {
	var args []Term
	var diags []Diagnostic
	if p.peekTok().kind == tPunct && p.peekTok().text == closing {
		p.advance()
		return args, diags, nil
	}
	for {
		// arguments bind below ',' (prec 999) so commas separate them.
		arg, d, err := p.term(999)
		diags = append(diags, d...)
		if err != nil {
			return nil, diags, err
		}
		args = append(args, arg)
		sep := p.advance()
		if sep.kind == tPunct && sep.text == "," {
			continue
		}
		if sep.kind == tPunct && sep.text == closing {
			return args, diags, nil
		}
		if sep.kind == tEOF || (sep.kind == tPunct && sep.text == ".") {
			p.unread(sep) // let resync see the terminator
		}
		return nil, diags, fmt.Errorf("expected ',' or %q in arguments, got %q", closing, sep.text)
	}
}

// list parses the remainder of a list after the opening '['.
func (p *parser) list() (Term, []Diagnostic, error) {
	var diags []Diagnostic
	if p.peekTok().kind == tPunct && p.peekTok().text == "]" {
		p.advance()
		return Nil, diags, nil
	}
	var items []Term
	tail := Term(Nil)
	for {
		it, d, err := p.term(999)
		diags = append(diags, d...)
		if err != nil {
			return nil, diags, err
		}
		items = append(items, it)
		sep := p.advance()
		switch {
		case sep.kind == tPunct && sep.text == ",":
			continue
		case sep.kind == tPunct && sep.text == "|":
			t, d2, err := p.term(999)
			diags = append(diags, d2...)
			if err != nil {
				return nil, diags, err
			}
			tail = t
			end := p.advance()
			if !(end.kind == tPunct && end.text == "]") {
				return nil, diags, fmt.Errorf("expected ']' after list tail")
			}
			return List(items, tail), diags, nil
		case sep.kind == tPunct && sep.text == "]":
			return List(items, tail), diags, nil
		default:
			return nil, diags, fmt.Errorf("expected ',', '|' or ']' in list, got %q", sep.text)
		}
	}
}

// splitClause turns a parsed term into (head, body). A ":-"/2 term is a rule;
// anything else is a fact.
func splitClause(t Term) (Term, []Term) {
	if c, ok := t.(Compound); ok && c.Functor == ":-" && len(c.Args) == 2 {
		return c.Args[0], flattenConj(c.Args[1])
	}
	return t, nil
}

// flattenConj flattens a right-nested ","/2 chain into a goal slice.
func flattenConj(t Term) []Term {
	c, ok := t.(Compound)
	if !ok || c.Functor != "," || len(c.Args) != 2 {
		return []Term{t}
	}
	return append([]Term{c.Args[0]}, flattenConj(c.Args[1])...)
}

// scanDiagnostics walks clause-body goals and reports the ones the engine will
// not execute, so nothing is dropped silently.
func scanDiagnostics(body []Term, line int) []Diagnostic {
	var out []Diagnostic
	var visit func(Term)
	visit = func(t Term) {
		switch x := t.(type) {
		case Atom:
			if x.Name == "!" {
				out = append(out, Diagnostic{DiagCut, "cut (!) is not implemented; ignored during resolution", line})
			}
			if x.Name == "nl" {
				out = append(out, Diagnostic{DiagIO, "nl/0 has no effect in this engine", line})
			}
		case Compound:
			switch Indicator(x) {
			case "write/1", "writeln/1", "print/1", "read/1", "format/1", "format/2", "format/3":
				out = append(out, Diagnostic{DiagIO, Indicator(x) + " (IO) is not executed", line})
			case "assert/1", "asserta/1", "assertz/1", "retract/1", "retractall/1", "abolish/1":
				out = append(out, Diagnostic{DiagDatabase, Indicator(x) + " (database mutation) is not executed", line})
			case "is/2", "</2", ">/2", "=</2", ">=/2", "=:=/2", "=\\=/2":
				out = append(out, Diagnostic{DiagArith, Indicator(x) + " (arithmetic) is not evaluated", line})
			case "findall/3", "bagof/3", "setof/3", "forall/2", "aggregate_all/3":
				out = append(out, Diagnostic{DiagUnsupported, Indicator(x) + " is not supported", line})
			}
			for _, a := range x.Args {
				visit(a)
			}
		}
	}
	for _, g := range body {
		visit(g)
	}
	return out
}
