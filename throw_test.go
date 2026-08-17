package prolog

import (
	"context"
	"errors"
	"testing"
)

func TestCatchRecovers(t *testing.T) {
	// A throw whose ball unifies with the catcher runs the recovery goal.
	if got := first(t, "", "catch(throw(oops), E, (R = caught(E)))", "R"); got != "caught(oops)" {
		t.Errorf("catch => %q, want caught(oops)", got)
	}
	// The catcher binds the ball.
	if got := first(t, "", "catch(throw(err(42)), err(X), true)", "X"); got != "42" {
		t.Errorf("catch binds ball => %q, want 42", got)
	}
}

func TestCatchLetsSolutionsThrough(t *testing.T) {
	// When Goal does not throw, catch is transparent: solutions flow through.
	prog := `p(1). p(2). p(3).`
	g, _, err := ParseTerm("catch(p(X), _, fail)")
	if err != nil {
		t.Fatal(err)
	}
	sols, err := NewMachine(Parse(prog).Clauses).Solve(context.Background(), []Term{g}, 0)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	got := make([]string, len(sols))
	for i, s := range sols {
		got[i] = s["X"].String()
	}
	if !eq(got, []string{"1", "2", "3"}) {
		t.Errorf("catch transparent => %v, want [1 2 3]", got)
	}
}

func TestCatcherMismatchRethrows(t *testing.T) {
	// The ball does not unify with the catcher, so the exception propagates.
	g, _, err := ParseTerm("catch(throw(boom), other(_), true)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewMachine(nil).Solve(context.Background(), []Term{g}, 1)
	ball, ok := Ball(err)
	if !ok {
		t.Fatalf("want uncaught prolog exception, got %v", err)
	}
	if ball.String() != "boom" {
		t.Errorf("rethrown ball => %q, want boom", ball.String())
	}
}

func TestThrowFromContinuationEscapesCatch(t *testing.T) {
	// A throw AFTER the catch (in the continuation) is not caught by that catch.
	g, _, err := ParseTerm("catch(true, _, fail), throw(after)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewMachine(nil).Solve(context.Background(), []Term{g}, 1)
	ball, ok := Ball(err)
	if !ok || ball.String() != "after" {
		t.Fatalf("continuation throw should escape catch; got err=%v", err)
	}
}

func TestArithmeticErrorsAreCatchable(t *testing.T) {
	// Division by zero throws evaluation_error(zero_divisor).
	if got := first(t, "", "catch(X is 1 // 0, error(E, _), true)", "E"); got != "evaluation_error(zero_divisor)" {
		t.Errorf("zero divisor => %q, want evaluation_error(zero_divisor)", got)
	}
	// Unbound variable in arithmetic throws instantiation_error.
	if got := first(t, "", "catch(X is Y + 1, error(E, _), true)", "E"); got != "instantiation_error" {
		t.Errorf("instantiation => %q, want instantiation_error", got)
	}
	// A non-evaluable atom throws type_error(evaluable, foo/0). (The indicator
	// prints in functional form since operators are not written infix.)
	if got := first(t, "", "catch(X is foo, error(type_error(evaluable, I), _), true)", "I"); got != "/(foo,0)" {
		t.Errorf("type_error culprit => %q, want /(foo,0)", got)
	}
}

func TestUncaughtSurfacesAsError(t *testing.T) {
	g, _, err := ParseTerm("throw(my_error)")
	if err != nil {
		t.Fatal(err)
	}
	_, solveErr := NewMachine(nil).Solve(context.Background(), []Term{g}, 1)
	if solveErr == nil {
		t.Fatal("uncaught throw should surface as an error")
	}
	if ball, ok := Ball(solveErr); !ok || ball.String() != "my_error" {
		t.Errorf("uncaught ball => %v, ok=%v", ball, ok)
	}
	// It is a prologThrow, distinct from ErrDepthExceeded.
	if errors.Is(solveErr, ErrDepthExceeded) {
		t.Error("throw should not be ErrDepthExceeded")
	}
}
