package panics_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gokern/panics"
)

func TestCatch_noPanicReturnsNil(t *testing.T) {
	t.Parallel()

	require.NoError(t, panics.Catch(func() {}))
}

func TestCatch_nilInterfaceOnSuccess(t *testing.T) {
	t.Parallel()

	// The trap the error return exists to avoid: a *Panic return would put a nil
	// pointer inside a non-nil interface, making `err != nil` true after a clean
	// run.
	err := panics.Catch(func() {})
	require.NoError(t, err)
	require.True(t, err == nil, "a clean Catch must produce a truly nil error") //nolint:testifylint
}

func TestCatchError_forwardsTheError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("returned")

	err := panics.CatchError(func() error { return sentinel })

	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, panics.ErrPanic, "a returned error is not a panic")
}

func TestCatchError_containsThePanic(t *testing.T) {
	t.Parallel()

	err := panics.CatchError(func() error { panic("boom") })

	require.ErrorIs(t, err, panics.ErrPanic)
	require.EqualError(t, err, "panic: boom")
}

//go:noinline
func raiseNil() { panic(nil) }

func TestCatchError_containsAPanicWithANilValue(t *testing.T) {
	// No t.Parallel here: t.Setenv forbids it, because GODEBUG is process-wide.
	//
	// Under panicnil=1, recover() returns nil for a panic that really happened.
	// Inferring "no panic" from that loses the panic completely: Catch returns
	// nil, the caller reports success, and the goroutine's work is gone with no
	// trace of why.
	t.Setenv("GODEBUG", "panicnil=1")

	err := panics.Catch(raiseNil)

	require.Error(t, err, "a panic with a nil value is still a panic")
	require.ErrorIs(t, err, panics.ErrPanic)
	require.EqualError(t, err, "panic: <nil>", "a nil value renders the way a nil Panic does")

	p, ok := panics.As(err)
	require.True(t, ok)
	require.Nil(t, p.Value, "the value recover() yielded is reported as it was")

	frame := topFrame(t, p)
	require.True(t, strings.HasSuffix(frame.Function, ".raiseNil"),
		"the fabricated capture must still point at the panic site, got %q", frame.Function)

	// The other half: the completion flag must not turn a clean run into a panic.
	// Without this, a fix for the above could report every success as a
	// nil-valued panic and the assertions above would not notice.
	require.NoError(t, panics.Catch(func() {}), "a clean run under panicnil=1 is still a clean run")

	sentinel := errors.New("returned")
	require.ErrorIs(t, panics.CatchError(func() error { return sentinel }), sentinel)
}

func TestIs(t *testing.T) {
	t.Parallel()

	require.True(t, panics.Is(panics.Catch(func() { panic("boom") })))
	require.False(t, panics.Is(errors.New("ordinary")))
	require.False(t, panics.Is(nil))
}

func TestAs(t *testing.T) {
	t.Parallel()

	err := panics.Catch(func() { panic("boom") })

	p, ok := panics.As(err)
	require.True(t, ok)
	require.Equal(t, "boom", p.Value)

	_, ok = panics.As(errors.New("ordinary"))
	require.False(t, ok)
}

func TestAs_throughAWrapper(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("callback panicked")

	err := fmt.Errorf("%w: %w", sentinel, panics.Catch(func() { panic("boom") }))

	require.ErrorIs(t, err, sentinel, "the wrapper's own sentinel survives")
	require.ErrorIs(t, err, panics.ErrPanic, "the core marker survives")

	p, ok := panics.As(err)
	require.True(t, ok, "As must reach through a package wrapper")
	require.Equal(t, "boom", p.Value)
}

func TestAs_typedNilReportsFalse(t *testing.T) {
	t.Parallel()

	// Reporting true here would hand every caller writing the natural
	// `p, ok := As(err); if ok { p.StackTrace() }` a pointer that nil-derefs on
	// its exported Value field.
	var typedNil *panics.Panic

	p, ok := panics.As(typedNil)
	require.False(t, ok, "a typed-nil *Panic is not a recovered panic")
	require.Nil(t, p, "a failed As must not hand back a pointer")

	p, ok = panics.As(fmt.Errorf("wrapped: %w", typedNil))
	require.False(t, ok, "As must not report success through a wrapper either")
	require.Nil(t, p)
}

func TestAs_reachesPastATypedNil(t *testing.T) {
	t.Parallel()

	// Matching on type alone (errors.AsType, or errors.As with a **Panic target)
	// stops at the first node whose type fits. A typed-nil *Panic shallower than
	// a real one then makes As report false while Is reports true: the caller is
	// told the error is a panic and cannot reach Value or StackTrace.
	var typedNil *panics.Panic

	chains := map[string]func(caught error) error{
		// errors.Join and fmt.Errorf's multi-%w build different concrete types,
		// both reached through Unwrap() []error.
		"join":           func(caught error) error { return errors.Join(typedNil, caught) },
		"multi wrap":     func(caught error) error { return fmt.Errorf("%w %w", typedNil, caught) },
		"join reversed":  func(caught error) error { return errors.Join(caught, typedNil) },
		"nested in wrap": func(caught error) error { return fmt.Errorf("x: %w", errors.Join(typedNil, caught)) },
	}

	for name, build := range chains {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := build(panics.Catch(func() { panic("boom") }))

			require.True(t, panics.Is(err), "the chain holds a real panic")

			p, ok := panics.As(err)
			require.True(t, ok, "As must skip the typed nil and keep walking")
			require.NotNil(t, p)
			require.Equal(t, "boom", p.Value)
			require.NotEmpty(t, p.StackTrace(), "the frames Is promised must be reachable")
		})
	}
}

func TestAs_chainWithoutAPanicReportsFalse(t *testing.T) {
	t.Parallel()

	var typedNil *panics.Panic

	chains := map[string]error{
		"nil":    nil,
		"single": errors.New("ordinary"),
		"wrapped": fmt.Errorf(
			"outer: %w",
			fmt.Errorf("inner: %w", errors.New("ordinary")),
		),
		"joined":             errors.Join(errors.New("first"), errors.New("second")),
		"only typed nils":    errors.Join(typedNil, typedNil),
		"typed nil and junk": errors.Join(typedNil, errors.New("ordinary")),
	}

	for name, err := range chains {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, ok := panics.As(err)
			require.False(t, ok, "no reachable *Panic in this chain")
			require.Nil(t, p, "a failed As must not hand back a pointer")
		})
	}
}

func TestAs_reportsTheOutermostPanic(t *testing.T) {
	t.Parallel()

	// A chain holding two is pathological: a contained panic re-raised and
	// contained again. The outer one is the recovery that produced this error,
	// and the inner one stays reachable through it.
	inner := panics.Catch(func() { panic("inner") })
	outer := panics.Catch(func() { panic(inner) })

	p, ok := panics.As(fmt.Errorf("wrap: %w", outer))
	require.True(t, ok)
	require.Equal(t, inner, p.Value, "As reports the outer recovery, whose value is the inner one")

	nested, isError := p.Value.(error)
	require.True(t, isError, "the outer recovery's value is the inner *Panic")

	innerPanic, ok := panics.As(nested)
	require.True(t, ok, "the inner panic stays reachable from the outer one")
	require.Equal(t, "inner", innerPanic.Value)
}

// Catch delegates to CatchError, which defers a closure, so this shape puts more
// frames between the panic and the capture than any case in capture_test.go. The
// fixture is that file's raise; what this adds is Catch's frame layout, not
// another panicking function.
func TestCatch_stackReachesThePanicSite(t *testing.T) {
	t.Parallel()

	err := panics.Catch(raise)

	p, ok := panics.As(err)
	require.True(t, ok)

	frame := topFrame(t, p)
	require.True(t, strings.HasSuffix(frame.Function, ".raise"),
		"the gopanic scan must reach the panic site through Catch's frames, got %q", frame.Function)
	require.False(t, strings.HasPrefix(frame.Function, "runtime."),
		"got %q", frame.Function)
}

// Exercises capture's gopanic-not-found path, which is documented behaviour: with
// no panic unwinding there is nothing to trim to, so the frames describe the call
// site instead of a panic site.
func TestRecover_outsideAPanicRetainsTheCallerStack(t *testing.T) {
	t.Parallel()

	p := panics.Recover("not actually a panic")

	frame := topFrame(t, p)

	// The expected frame is named rather than gopanic merely ruled out, because
	// no gopanic is on this stack at all: an assertion against it would hold
	// whatever the fallback retained, panics.capture included. This is also the
	// one assertion that pins capture's skipSelf, which has no gopanic to
	// re-anchor on here if the count is wrong.
	require.True(
		t,
		strings.HasSuffix(frame.Function, ".TestRecover_outsideAPanicRetainsTheCallerStack"),
		"the fallback must retain the caller's stack, starting at the caller, got %q",
		frame.Function,
	)
	require.NotContains(t, frame.Function, "gokern/panics.",
		"the fallback must not lead with this package's own frames, got %q", frame.Function)
}
