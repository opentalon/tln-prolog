package prolog

import (
	"context"
	"testing"
)

func TestLengthBidirectional(t *testing.T) {
	// Measure: list given.
	if got := first(t, "", "length([a,b,c], N)", "N"); got != "3" {
		t.Errorf("length measure => %q, want 3", got)
	}
	// Generate: count given, produce a list of N slots (here filled to check length).
	if got := first(t, "", "length(L, 3), L = [a,b,c]", "L"); got != "[a,b,c]" {
		t.Errorf("length generate => %q, want [a,b,c]", got)
	}
	// A wrong length fails to unify.
	if got := first(t, "", "length(L, 2), L = [a,b,c]", "L"); got != "<none>" {
		t.Errorf("length 2 vs 3-list => %q, want <none>", got)
	}
	// Zero.
	if got := first(t, "", "length(L, 0)", "L"); got != "[]" {
		t.Errorf("length 0 => %q, want []", got)
	}
}

func TestLengthEnumerates(t *testing.T) {
	// Both unbound: enumerate increasing lengths (cap the stream).
	g, _, err := ParseTerm("length(L, N)")
	if err != nil {
		t.Fatal(err)
	}
	sols, err := NewMachine(nil).Solve(context.Background(), []Term{g}, 3)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(sols))
	for i, s := range sols {
		got[i] = s["N"].String()
	}
	if !eq(got, []string{"0", "1", "2"}) {
		t.Errorf("length enumerate => %v, want [0 1 2]", got)
	}
}

func TestNthBidirectional(t *testing.T) {
	// Index mode.
	if got := first(t, "", "nth0(1, [a,b,c], X)", "X"); got != "b" {
		t.Errorf("nth0 index => %q, want b", got)
	}
	if got := first(t, "", "nth1(1, [a,b,c], X)", "X"); got != "a" {
		t.Errorf("nth1 index => %q, want a", got)
	}
	// Search mode: find the index of an element.
	if got := first(t, "", "nth0(N, [a,b,c], c)", "N"); got != "2" {
		t.Errorf("nth0 search => %q, want 2", got)
	}
	if got := first(t, "", "nth1(N, [a,b,c], c)", "N"); got != "3" {
		t.Errorf("nth1 search => %q, want 3", got)
	}
}

func TestNthEnumerates(t *testing.T) {
	// Fully unbound index: enumerate (index, element) pairs.
	g, _, err := ParseTerm("nth0(N, [x,y,z], E)")
	if err != nil {
		t.Fatal(err)
	}
	sols, err := NewMachine(nil).Solve(context.Background(), []Term{g}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var pairs []string
	for _, s := range sols {
		pairs = append(pairs, s["N"].String()+":"+s["E"].String())
	}
	if !eq(pairs, []string{"0:x", "1:y", "2:z"}) {
		t.Errorf("nth0 enumerate => %v, want [0:x 1:y 2:z]", pairs)
	}
}
