package panics_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gokern/panics"
)

// frameNames names every frame p retained, innermost first. Names are all these
// tests read of a frame. Resolving them once here keeps the assertions about
// stack shape from having to be written as loops.
func frameNames(t *testing.T, p *panics.Panic) []string {
	t.Helper()

	require.NotNil(t, p, "a recovered panic must produce a *Panic")

	stack := p.StackTrace()
	require.NotEmpty(t, stack, "a recovered panic must carry frames")

	var (
		names  []string
		frames = runtime.CallersFrames(stack)
	)

	for {
		frame, more := frames.Next()
		names = append(names, frame.Function)

		if !more {
			break
		}
	}

	return names
}

// caughtFrameNames names the frames of the panic err carries.
func caughtFrameNames(t *testing.T, err error) []string {
	t.Helper()

	p, ok := panics.As(err)
	require.True(t, ok, "expected a recovered panic")

	return frameNames(t, p)
}

// topFrameName is the name of the first frame p retained.
func topFrameName(t *testing.T, p *panics.Panic) string {
	t.Helper()

	return frameNames(t, p)[0]
}

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
	// past its bound, retaining gopanic and this package's own frames. The depths
	// here spread wide enough that such a bound cannot hide in the shallow cases.
	for _, depth := range []int{5, 12, 32} {
		t.Run(fmt.Sprintf("depth %d", depth), func(t *testing.T) {
			t.Parallel()

			names := frameNames(t, deepDefer(depth))

			require.True(t, strings.HasSuffix(names[0], ".raise"),
				"top frame must be the function that called panic, got %q", names[0])
			require.NotContains(
				t,
				names[0],
				"gokern/panics.",
				"the retained stack must not lead with this package's own frames, got %q",
				names[0],
			)
			require.NotContains(t, names, "runtime.gopanic",
				"gopanic must be trimmed off however deep the recover site is")
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

			top := topFrameName(t, shape())

			require.False(t, strings.HasPrefix(top, "runtime."),
				"top frame must not be runtime internals, got %q", top)
			require.True(t, strings.HasSuffix(top, ".raise"),
				"top frame must be the function that called panic, got %q", top)
		})
	}
}

func TestCapture_neverRetainsGopanic(t *testing.T) {
	t.Parallel()

	require.NotContains(t, frameNames(t, directDefer()), "runtime.gopanic",
		"gopanic must be trimmed off the retained stack")
}

func TestRecover_nilReturnsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, panics.Recover(nil))
}

// The bottom trim is the mirror of the gopanic trim at the top: where that one
// drops the runtime's way in, this one drops our way out. What survives is the
// panic's own story and nothing else.
func TestCapture_stopsAtTheContainmentBoundary(t *testing.T) {
	t.Parallel()

	t.Run("Catch retains only the panic's own frames", func(t *testing.T) {
		t.Parallel()

		// Pinned as an exact list rather than as an absence: this is the shape
		// where every frame the trim should have removed can be named, so
		// asserting the whole of what is left says more than ruling frames out
		// one at a time.
		require.Equal(t,
			[]string{"github.com/gokern/panics_test.raise"},
			caughtFrameNames(t, panics.Catch(raise)),
			"Catch calls raise directly, so raise is the whole story")
	})

	t.Run("no machinery of ours survives", func(t *testing.T) {
		t.Parallel()

		names := caughtFrameNames(t, panics.Catch(func() { raiseAtDepth(3) }))

		// Counted, not pattern-matched. A leaked frame here would be the closure
		// Catch defers, and the check below cannot see one: the compiler names a
		// closure after whoever called it, so it carries the caller's package and
		// not ours. Its spelling is no help either. The same closure resolves as
		// "…TestName.Catch.func3" from a test body and "…TestName.func1.Catch.2"
		// from inside a t.Run, so any marker drawn from it is a guess about the
		// compiler. The number of frames that belong here does not move.
		require.Len(t, names, 5,
			"four raiseAtDepth frames and the closure that called them, and nothing else")

		for _, name := range names[:4] {
			require.Equal(t, "github.com/gokern/panics_test.raiseAtDepth", name,
				"the panic's own frames come first and unbroken")
		}

		for _, name := range names {
			require.NotContains(t, name, "gokern/panics.",
				"a frame of the containment machinery reached the caller")
		}
	})
}

func TestRecover_hasNoContainmentBoundary(t *testing.T) {
	t.Parallel()

	// Nothing of ours sits below the panic site here, so there is no boundary to
	// find and the whole stack is kept. Documented, not an oversight: only the
	// caller knows where their guard begins.
	var p *panics.Panic

	func() {
		defer func() { p = panics.Recover(recover()) }()

		raise()
	}()

	require.Greater(t, len(frameNames(t, p)), 1,
		"an unassisted Recover has no boundary to trim to")
}

// The capture window is finite and the boundary sits below every frame the panic
// unwound through, so a long enough chain pushes the boundary off the end and the
// bottom trim finds nothing to cut. What would have been trimmed is exactly what
// did not fit, so the stack comes back short of the boundary rather than running
// past it.
func TestCapture_aPanicDeeperThanTheWindowIsTruncatedNotLeaked(t *testing.T) {
	t.Parallel()

	// No assertion here about testing.tRunner or runtime.goexit: 200 frames of
	// recursion overflow the window long before it reaches them, so ruling them
	// out would hold against any implementation, correct or not. What is left
	// after the window truncates is all this shape can speak to.
	names := caughtFrameNames(t, panics.Catch(func() { raiseAtDepth(200) }))

	for _, name := range names {
		require.NotContains(t, name, "gokern/panics.",
			"the window ran out before the boundary, so no frame of ours can be here")
	}

	require.Equal(t, "github.com/gokern/panics_test.raiseAtDepth", names[len(names)-1],
		"the window ran out mid-unwind, so the last frame is still the panicking recursion")
}

// The window bounds how deep the recover site can be, not just how deep the panic
// is: every frame between the deferred function and Recover is one the capture
// has to hold before it reaches gopanic. Past that the panic site is outside the
// capture, and this package cannot reconstruct that stack. It must not return an
// empty one either. gopanic landing in the last slot used to produce exactly
// that: a *Panic reporting a panic and carrying no evidence of it.
func TestCapture_aDeepRecoverSiteStillCarriesFrames(t *testing.T) {
	t.Parallel()

	// Swept rather than sampled at the depth that broke, because that depth is a
	// property of maxFrames and of how many frames the compiler gives each level.
	// Pin it and the test pins the build instead of the behaviour. frameNames
	// fails on an empty stack, so the sweep asserts as it goes.
	var alone bool

	for nesting := 1; nesting <= 128; nesting++ {
		names := frameNames(t, deepDefer(nesting))

		if len(names) == 1 {
			require.True(t, strings.HasSuffix(names[0], ".raise"),
				"the one frame left must be the panic site, got %q", names[0])

			alone = true
		}
	}

	// Somewhere in that sweep the window holds the panic site and nothing else.
	// Passing through that point says the fallback waits until the panic site is
	// genuinely outside the window. Give up one frame early and the retained count
	// jumps from two straight to the whole window, never landing on one.
	require.True(t, alone,
		"the trim must hold until only the panic site is left, not give up a frame early")
}

// A nil callback faults inside this package's own closure, so the panic site and
// the containment boundary are one frame. Trimming there leaves the runtime's
// fault frames and nothing that says who passed the nil, so the bottom trim
// stands down and the whole stack is kept.
func TestCapture_aPanicInsideTheGuardKeepsTheWholeStack(t *testing.T) {
	t.Parallel()

	// Both built here rather than inside the subtests, so the frame that has to
	// survive is this function's and not a subtest closure's.
	caught := map[string]error{
		"Catch":      panics.Catch(nil),
		"CatchError": panics.CatchError(nil),
	}

	const passedTheNil = "github.com/gokern/panics_test." +
		"TestCapture_aPanicInsideTheGuardKeepsTheWholeStack"

	for name, err := range caught {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, caughtFrameNames(t, err), passedTheNil,
				"the only frame naming whoever passed the nil must survive")
		})
	}
}

// The stand-down above keys on there being nothing but the runtime above the
// boundary, and the runtime raises plenty of panics inside a caller's own code.
// Those must still be trimmed, so this is what stops the rule from widening into
// "any panic that leads with a runtime frame".
func TestCapture_aRuntimePanicInCallerCodeIsStillTrimmed(t *testing.T) {
	t.Parallel()

	// Keyed by the fixture's own name, so the last assertion can say which frame
	// it expects to find rather than merely that something of the caller's is
	// there.
	faults := map[string]func(){
		"raiseIndexOutOfRange": raiseIndexOutOfRange,
		"raiseNilMapWrite":     raiseNilMapWrite,
		"raiseDivideByZero":    raiseDivideByZero,
	}

	for name, fault := range faults {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			names := caughtFrameNames(t, panics.Catch(fault))

			for _, frame := range names {
				require.NotContains(t, frame, "gokern/panics.",
					"the fault is the caller's, so the trim must still run")
			}

			require.NotContains(t, names, "testing.tRunner",
				"the caller's stack is below the boundary and must not survive")
			require.Equal(t, "github.com/gokern/panics_test."+name, names[len(names)-1],
				"the frame that faulted is the deepest thing the panic unwound through")
		})
	}
}
