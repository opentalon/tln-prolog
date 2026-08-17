package prolog

import "testing"

func TestOperatorWriting(t *testing.T) {
	cases := []struct{ src, want string }{
		{"foo/3", "foo/3"},
		{"1+2*3", "1+2*3"},
		{"(1+2)*3", "(1+2)*3"}, // parens preserved where precedence needs them
		{"a-b-c", "a-b-c"},     // left-assoc: no parens
		{"X = Y", "X=Y"},
		{"X is Y+1", "X is Y+1"}, // alphabetic operator spaced
		{"a mod b", "a mod b"},
		{"\\+ p(x)", "\\+p(x)"},
		{"foo(a, b, c)", "foo(a,b,c)"}, // plain compound stays functional
		{"[a,b,c]", "[a,b,c]"},         // lists unaffected
		{"[a,b|T]", "[a,b|T]"},
		{"f(1+2, g(x))", "f(1+2,g(x))"}, // operators inside arguments
		{"a:-b,c", "a:-b,c"},
	}
	for _, c := range cases {
		tm, diags, err := ParseTerm(c.src)
		if err != nil {
			t.Fatalf("parse %q: %v (%v)", c.src, err, diags)
		}
		if got := tm.String(); got != c.want {
			t.Errorf("write %q => %q, want %q", c.src, got, c.want)
		}
	}
}
