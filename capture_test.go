package panics_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gokern/panics"
)

// topFrame resolves the first retained frame of p's stack.
func topFrame(t *testing.T, p *panics.Panic) runtime.Frame {
	t.Helper()

	require.NotNil(t, p, "a recovered panic must produce a *Panic")

	stack := p.StackTrace()
	require.NotEmpty(t, stack, "a recovered panic must carry frames")

	frame, _ := runtime.CallersFrames(stack).Next()

	return frame
}

//go:noinline
func raise() { panic("boom") }

// The three shapes below differ in what sits between the deferred function and
// Recover: nothing, an anonymous closure, or a named //go:noinline helper. The
// last two add one frame each, so they differ in call-graph shape rather than in
// depth. No hand-tuned skip constant is right for all three; locating
// runtime.gopanic is right for any of them.
//
// Each takes a named return because Recover writes into it from the deferred
// closure, which is also what makes every "return nil" below unreachable.

func directDefer() (p *panics.Panic) { //nolint:nonamedreturns // written by the deferred closure below.
	defer func() {
		p = panics.Recover(recover())
	}()

	raise()

	return nil
}

func nestedDefer() (p *panics.Panic) { //nolint:nonamedreturns // written by the deferred closure below.
	defer func() {
		// The builtin stays directly in the deferred function, where it is the
		// only place it works. Only the call to Recover moves into the inner
		// closure, which is the extra frame this shape is testing.
		recovered := recover()

		func() {
			p = panics.Recover(recovered)
		}()
	}()

	raise()

	return nil
}

//go:noinline
func viaHelper(recovered any) *panics.Panic { return panics.Recover(recovered) }

func helperDefer() (p *panics.Panic) { //nolint:nonamedreturns // written by the deferred closure below.
	defer func() {
		p = viaHelper(recover())
	}()

	raise()

	return nil
}

// viaDepth calls itself down to n == 0 and recovers there, which makes the
// number of intervening frames a test parameter rather than a property of how
// these helpers happen to be written. //go:noinline keeps each level a real
// frame.
//
//go:noinline
func viaDepth(n int, recovered any) *panics.Panic {
	if n == 0 {
		return panics.Recover(recovered)
	}

	return viaDepth(n-1, recovered)
}

func deepDefer(depth int) (p *panics.Panic) { //nolint:nonamedreturns // see directDefer.
	defer func() {
		p = viaDepth(depth, recover())
	}()

	raise()

	return nil
}

func TestCapture_topFrameIsThePanicSiteAtAnyNestingDepth(t *testing.T) {
	t.Parallel()

	// A bounded scan for gopanic satisfies the shallow shapes above and then quits
	// past its bound, retaining gopanic and this package's own frames. These
	// depths straddle the bound of 8 that used to exist, so that failure cannot
	// hide in the shallow cases.
	for _, depth := range []int{5, 12, 32} {
		t.Run(fmt.Sprintf("depth %d", depth), func(t *testing.T) {
			t.Parallel()

			p := deepDefer(depth)

			frame := topFrame(t, p)
			require.True(t, strings.HasSuffix(frame.Function, ".raise"),
				"top frame must be the function that called panic, got %q", frame.Function)
			require.NotContains(
				t,
				frame.Function,
				"gokern/panics.",
				"the retained stack must not lead with this package's own frames, got %q",
				frame.Function,
			)

			frames := runtime.CallersFrames(p.StackTrace())

			for {
				current, more := frames.Next()
				require.NotEqual(t, "runtime.gopanic", current.Function,
					"gopanic must be trimmed off however deep the recover site is")

				if !more {
					break
				}
			}
		})
	}
}

func TestCapture_topFrameIsThePanicSite(t *testing.T) {
	t.Parallel()

	shapes := map[string]func() *panics.Panic{
		"direct defer": directDefer,
		"nested defer": nestedDefer,
		"via helper":   helperDefer,
	}

	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			frame := topFrame(t, shape())

			require.False(t, strings.HasPrefix(frame.Function, "runtime."),
				"top frame must not be runtime internals, got %q", frame.Function)
			require.True(t, strings.HasSuffix(frame.Function, ".raise"),
				"top frame must be the function that called panic, got %q", frame.Function)
		})
	}
}

func TestCapture_neverRetainsGopanic(t *testing.T) {
	t.Parallel()

	p := directDefer()
	require.NotNil(t, p)

	frames := runtime.CallersFrames(p.StackTrace())

	for {
		frame, more := frames.Next()
		require.NotEqual(t, "runtime.gopanic", frame.Function,
			"gopanic must be trimmed off the retained stack")

		if !more {
			break
		}
	}
}

func TestRecover_nilReturnsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, panics.Recover(nil))
}
