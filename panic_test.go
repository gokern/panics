package panics //nolint:testpackage // white-box: exercises Panic's unexported stack field.

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const boom = "boom"

// errUnrelated is a sentinel no chain in this file carries, so errors.Is has
// to walk every link of one before it can answer false.
var errUnrelated = errors.New("unrelated")

func TestPanic_Error(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value any
		want  string
	}{
		"string value":  {value: boom, want: "panic: boom"},
		"error value":   {value: errors.New("wrapped"), want: "panic: wrapped"},
		"integer value": {value: 42, want: "panic: 42"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &Panic{Value: tc.value}
			require.EqualError(t, p, tc.want)
		})
	}
}

func TestPanic_StructValueStaysReadable(t *testing.T) {
	t.Parallel()

	// %#v spelling of a composite is the runtime's business, not this
	// package's contract; assert the field survives, not the punctuation.
	p := &Panic{Value: struct{ Code int }{Code: 7}}

	require.Contains(t, p.Error(), "Code")
	require.Contains(t, p.Error(), "7")
}

func TestPanic_StackTraceIsACopy(t *testing.T) {
	t.Parallel()

	// Fixture counters: not real ones, only distinct nonzero values whose
	// mutation is observable.
	p := &Panic{Value: boom, stack: []uintptr{0x1, 0x2, 0x3}}

	first := p.StackTrace()
	require.NotEmpty(t, first)

	first[0] = 0

	second := p.StackTrace()
	require.NotZero(t, second[0], "StackTrace must hand out a copy")
}

func TestPanic_ValueIsPreserved(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("original")

	p := &Panic{Value: sentinel}
	require.Equal(t, sentinel, p.Value)
}

func TestPanic_UnwrapReachesErrPanic(t *testing.T) {
	t.Parallel()

	p := &Panic{Value: boom}

	require.ErrorIs(t, p, ErrPanic)
}

func TestPanic_UnwrapReachesTheCause(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("cause")

	p := &Panic{Value: fmt.Errorf("layer: %w", sentinel)}

	require.ErrorIs(t, p, ErrPanic, "the panic marker must survive")
	require.ErrorIs(t, p, sentinel, "the panic value's own chain must survive")
}

func TestPanic_TypedNilCauseIsNotUnwrapped(t *testing.T) {
	t.Parallel()

	p := &Panic{Value: (*os.PathError)(nil)}

	// The load-bearing assertion: the list must hold the marker alone. An
	// ErrorIs on ErrPanic cannot say this, because ErrPanic is index 0 and
	// errors.Is returns on the first match either way.
	require.Len(t, p.Unwrap(), 1, "a typed-nil cause must not join the unwrap list")

	var pathErr *os.PathError

	require.NotErrorAs(t, p, &pathErr,
		"unwrapping a typed nil would satisfy errors.As and hand back a nil pointer")

	// What the guard actually buys: errors.Is walks the whole list looking
	// for a sentinel it will not find, and a retained typed-nil cause would
	// put (*os.PathError)(nil).Unwrap() on that walk.
	require.NotPanics(t, func() { _ = errors.Is(p, errUnrelated) },
		"a miss must walk the full unwrap list without dereferencing the typed nil")

	require.ErrorIs(t, p, ErrPanic)
	require.NotPanics(t, func() { _ = p.Error() },
		"rendering a typed-nil panic value must not panic")
	require.Contains(t, p.Error(), "PathError")
}

// renderPanicError is a non-nil struct holding a nil field that its Error method
// dereferences. isTypedNil says false for it, correctly: the value is not nil,
// only its insides are.
type renderPanicError struct{ inner error }

func (p renderPanicError) Error() string { return "wrap: " + p.inner.Error() }

// goStringPanics reaches valueString's default branch instead, where fmt is
// asked for %#v and calls GoString.
type goStringPanics struct{}

func (goStringPanics) GoString() string { panic("GoString") }

func TestPanic_RenderingAMisbehavingValueDoesNotPanic(t *testing.T) {
	t.Parallel()

	// The crash this prevents lands wherever the error is rendered: a log call,
	// or sentry-go's convertErrorDFS, both far from the panic that produced it.
	values := map[string]any{
		"error method panics":    renderPanicError{},
		"gostring method panics": goStringPanics{},
		"error method is fine":   errors.New("ordinary"),
	}

	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &Panic{Value: value}

			require.NotPanics(t, func() { _ = p.Error() },
				"rendering a panic value must never panic")
			require.NotPanics(t, func() { _ = errors.Is(p, errUnrelated) })
			require.ErrorIs(t, p, ErrPanic, "the marker survives a value that will not render")
		})
	}

	// The message also stays diagnostic: fmt's marker names the method that
	// failed, so it says what happened instead of going empty.
	require.Contains(t, (&Panic{Value: renderPanicError{}}).Error(), "PANIC=")
	require.Contains(t, (&Panic{Value: goStringPanics{}}).Error(), "PANIC=")

	// An error that renders normally still renders as its message, not as a
	// struct dump.
	require.EqualError(t, &Panic{Value: errors.New("ordinary")}, "panic: ordinary")
}

func TestPanic_NilReceiverMethodsAreSafe(t *testing.T) {
	t.Parallel()

	// A typed-nil *Panic needs nothing more exotic than
	// werr.Wrap((*panics.Panic)(nil)) to reach a consumer, and every method has
	// to survive being called on one.
	var p *Panic

	require.NotPanics(t, func() { _ = p.Error() })
	require.Equal(t, "panic: <nil>", p.Error())

	require.NotPanics(t, func() { _ = p.Unwrap() })
	require.Nil(t, p.Unwrap(), "a nil Panic carries no marker and no cause")

	require.NotPanics(t, func() { _ = p.StackTrace() })
	require.Nil(t, p.StackTrace(), "a nil Panic carries no frames")
}

func TestPanic_NilReceiverSurvivesErrorsIs(t *testing.T) {
	t.Parallel()

	// Worse than the As trap: without the guard, errors.Is(err, anything) panics
	// for any chain holding a typed-nil Panic, because the walk calls Unwrap,
	// which dereferences p.Value.
	var p *Panic

	require.NotPanics(t, func() { _ = errors.Is(p, errUnrelated) })
	require.NotErrorIs(t, p, errUnrelated)
	require.NotErrorIs(t, p, ErrPanic,
		"a typed-nil Panic unwraps to nothing, so it carries no marker")

	// The same walk, reached the way a consumer would: through a wrapper.
	wrapped := fmt.Errorf("layer: %w", p)

	require.NotPanics(t, func() { _ = errors.Is(wrapped, errUnrelated) })
	require.NotErrorIs(t, wrapped, errUnrelated)
}

func TestPanic_TypedNilKinds(t *testing.T) {
	t.Parallel()

	// Every nilable kind that can satisfy error. reflect.Value.IsNil panics
	// on any other kind, which is why the guard enumerates exactly these.
	values := map[string]any{
		"pointer": (*os.PathError)(nil),
		"func":    (nilFuncError)(nil),
		"slice":   (nilSliceError)(nil),
		"map":     (nilMapError)(nil),
		"chan":    (nilChanError)(nil),
	}

	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := &Panic{Value: value}

			// NotPanics alone is not enough here: these Error() methods
			// return a constant string without touching the receiver, so
			// calling them on a nil value never panics either way. Asserting
			// the rendering carries the value's own type name proves the
			// %#v branch ran instead of the naive Error() call.
			require.NotPanics(t, func() { _ = p.Error() })
			require.Contains(t, p.Error(), fmt.Sprintf("%T", value))
			require.ErrorIs(t, p, ErrPanic)
		})
	}
}

type nilFuncError func()

func (nilFuncError) Error() string { return "func" }

type nilSliceError []int

func (nilSliceError) Error() string { return "slice" }

type nilMapError map[string]int

func (nilMapError) Error() string { return "map" }

type nilChanError chan int

func (nilChanError) Error() string { return "chan" }
