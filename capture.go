package panics

import (
	"runtime"
	"slices"
)

// maxFrames bounds the capture, not the retained depth. runtime.Callers writes
// the innermost frames first, the ones that say where the panic came from, so a
// deeper call chain loses only outer frames.
//
// The trim below then drops everything through runtime.gopanic, and this budget
// is spent before that trim runs. A stack that filled the window therefore
// retains fewer than maxFrames frames, by a margin that depends on how many the
// caller put between its deferred function and Recover, so a short retained stack
// does not imply a shallow goroutine stack.
const maxFrames = 64

// gopanicName is the runtime function every panic unwinds through, and the anchor
// that capture trims to.
const gopanicName = "runtime.gopanic"

// skipSelf drops runtime.Callers, capture, and capture's caller, so the capture
// starts at the code that called in. Every call site is therefore a function of
// this package invoked directly by the code whose stack is wanted: Recover, or the
// closure CatchError defers.
//
// This is the one hard-coded frame count here, and it spans this package's own
// frames, never a caller's. On the panic path a wrong count hides behind the
// gopanic scan, which re-anchors the stack either way. Nothing repairs it on the
// fallback path, so TestRecover_outsideAPanicRetainsTheCallerStack asserts the
// top retained frame is its own; that fails for a count wrong in either
// direction.
const skipSelf = 3

// Recover converts a value from recover() into a *Panic carrying the panicking
// goroutine's stack:
//
//	defer func() {
//	    if p := panics.Recover(recover()); p != nil {
//	        err = p
//	    }
//	}()
//
// Recover must be called while the panic is still unwinding, meaning from inside
// a deferred function, at any nesting depth. Called after that function has
// returned, it captures the surrounding call stack instead of the panic site.
// Recover(nil) returns nil, so the guard above is all a caller has to write.
//
// The recover() builtin is stricter: it has to be called directly by the deferred
// function. One call deeper it returns nil, this function returns nil with it,
// the panic keeps unwinding, and the process dies. That version compiles and
// passes any test that does not actually panic, so only the call to Recover may
// move:
//
//	// Works: recover() sits in the deferred function, Recover is a frame deeper.
//	defer func() {
//	    recovered := recover()
//	    err = translate(panics.Recover(recovered))
//	}()
//
// The result is a concrete *Panic rather than an error because it is examined
// immediately by the nil check above. Catch and CatchError return error, since
// their results flow onward.
func Recover(recovered any) *Panic {
	if recovered == nil {
		return nil
	}

	return &Panic{Value: recovered, stack: capture()}
}

// capture records the panicking goroutine's stack, trimmed so the first retained
// frame is the function that called panic.
//
// Past this package's own frames (see skipSelf), the panic site is located by
// scanning for runtime.gopanic rather than by a constant. A constant would need
// re-deriving every time a frame is added between the deferred function and this
// one, and a stale one shows up only when somebody reads a bad stack.
//
// The scan is unbounded because the distance it covers is the caller's choice,
// not this package's: Recover works from a deferred function at any nesting
// depth, and every frame in between pushes gopanic one further out. A bound would
// cap the depth at which that works, without saying so. Past it the trim stops,
// the retained stack
// leads with runtime.gopanic and this package's own frames, and a consumer's
// dashboard blames the panic on panics instead of on the code that raised it.
// Scanning further costs nothing on a real panic, since the loop stops at
// gopanic. Measured with BenchmarkCatch_panic and BenchmarkRecover_deferredAtDepth,
// unbounded and the bound of eight it replaced are indistinguishable in time.
//
// When gopanic is not on the stack at all the whole capture is retained, the one
// case that resolves every captured frame. That means Recover was called outside
// an unwinding panic, where there is no panic site to trim to.
func capture() []uintptr {
	var pcs [maxFrames]uintptr

	depth := runtime.Callers(skipSelf, pcs[:])
	if depth == 0 {
		return nil
	}

	for i := range depth {
		// pc-1 because a return address points just past the call, and the
		// byte after a call can belong to the next function.
		fn := runtime.FuncForPC(pcs[i] - 1)
		if fn != nil && fn.Name() == gopanicName {
			return slices.Clone(pcs[i+1 : depth])
		}
	}

	return slices.Clone(pcs[:depth])
}
