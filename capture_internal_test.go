package panics

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The anchor is read from the compiler's own record, so an empty one means only
// that runtime.Caller declined to answer at init. That is not reachable from
// outside the package, and it is the one state in which the bottom trim has no
// idea where the boundary is, so what it does then is worth stating rather than
// inferring: it keeps everything, the same as a panic caught outside this
// package's guards.
//
//nolint:paralleltest // blanking catchFile is only safe while the parallel tests are paused.
func TestCapture_withoutAnAnchorKeepsTheWholeStack(t *testing.T) {
	// No t.Parallel here: catchFile is package state, and blanking it is only
	// safe while every other test is still paused. That is the same guarantee
	// TestCatchError_containsAPanicWithANilValue leans on for t.Setenv.
	require.NotEmpty(t, catchFile, "runtime.Caller must have answered at init")

	trimmed := containedFrameNames(t)

	original := catchFile
	catchFile = ""

	t.Cleanup(func() { catchFile = original })

	whole := containedFrameNames(t)

	require.NotContains(t, trimmed, "runtime.goexit",
		"with an anchor the trim runs and the caller's stack is gone")
	require.Contains(t, whole, "runtime.goexit",
		"without one it does not run, and the goroutine stack is kept to the bottom")
	require.Contains(t, whole, "github.com/gokern/panics.CatchError",
		"the machinery the anchor exists to remove is what stays behind without it")
	require.Greater(t, len(whole), len(trimmed),
		"keeping everything must retain strictly more than trimming")
}

// skipSelf is one hard-coded count serving both entry points, so the two have to
// sit at one call depth. TestRecover_outsideAPanicRetainsTheCallerStack pins
// Recover from outside the package, and nothing pinned contained against it: a
// frame inserted ahead of capture on the contained path passed the whole suite
// clean.
//
// Deliberately off a panic path. With no gopanic to re-anchor on, a wrong count
// lands in the retained stack instead of being absorbed by the trim.
func TestSkipSelf_containedSitsAtRecoverDepth(t *testing.T) {
	t.Parallel()

	viaRecover := Recover("not actually a panic").StackTrace()
	viaContained := contained("not actually a panic").StackTrace()

	require.NotEmpty(t, viaRecover, "the fallback must retain the caller's stack")
	require.Len(t, viaContained, len(viaRecover),
		"both entry points must open the window on the same frame")
	require.Equal(t, topFrameName(t, viaRecover), topFrameName(t, viaContained),
		"both must start at this test, not at a frame of ours")
	require.True(
		t,
		strings.HasSuffix(topFrameName(t, viaRecover), ".TestSkipSelf_containedSitsAtRecoverDepth"),
		"skipSelf must land the window on the caller, got %q",
		topFrameName(t, viaRecover),
	)
}

// A pc the runtime cannot place resolves to no function, and containmentSite has
// to step over it rather than treat it as a frame that failed to match. Reached
// directly because a real capture holds only pcs the runtime has just produced,
// so nothing on a live stack exercises the skip.
func TestContainmentSite_skipsAnUnresolvablePC(t *testing.T) {
	t.Parallel()

	require.Equal(t, 3, containmentSite([]uintptr{1, 1, 1}, "somefile.go"),
		"frames the runtime cannot place are stepped over, not matched")
}

// containedFrameNames names the frames of a panic contained by Catch, read the way a
// caller reads them.
//
// A near-twin of capture_test.go's frameNames, and unavoidably so: that one lives
// in panics_test and this file has to be in panics to reach catchFile at all.
// Neither package can see the other's helpers.
func containedFrameNames(t *testing.T) []string {
	t.Helper()

	p, ok := As(Catch(func() { panic("boom") }))
	require.True(t, ok, "Catch must report the panic")

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

// topFrameName is the name of the first frame in pcs. Deliberately the same name
// as capture_test.go's helper: the two cannot share code across the package
// boundary, so sharing the name is what says they answer the same question.
func topFrameName(t *testing.T, pcs []uintptr) string {
	t.Helper()

	require.NotEmpty(t, pcs, "an empty stack has no top frame")

	frame, _ := runtime.CallersFrames(pcs).Next()

	return frame.Function
}
