package panics

import (
	"runtime"
	"slices"
	"strings"
)

// maxFrames bounds the capture, not the retained depth. runtime.Callers writes
// the innermost frames first, the ones that say where the panic came from, so a
// deeper call chain loses only outer frames.
//
// The trims below then drop everything through runtime.gopanic at the top and
// everything from the containment boundary down, and this budget is spent before
// either runs. Each trim that fires shortens the result: the top one by the
// frames above the panic site, the bottom one by everything below the boundary,
// and the bottom one fires only if the boundary fit in the window at all. So a
// short retained stack does not imply a shallow goroutine stack, and a result of
// exactly maxFrames means neither trim fired. See panicSite for when that
// happens and what it costs.
//
// It also bounds how deep a recover site can be. Every frame a caller puts
// between its deferred function and Recover is one more the window has to hold
// before gopanic, so past roughly maxFrames of nesting the panic site is outside
// the capture and the retained stack describes the recovery instead. Raising this
// raises that ceiling; the cost is the array below, on the stack of every
// recovered panic.
const maxFrames = 64

// gopanicName is the runtime function every panic unwinds through, and the anchor
// panicSite trims the top of the stack to.
const gopanicName = "runtime.gopanic"

// runtimePrefix matches the frames the runtime pushes ahead of the one that
// faulted (sigpanic, panicmem, panicdivide and their kin) when it raises a panic
// itself rather than the code calling panic for it.
//
// Only the standard library's runtime is named this bare. A caller's own package
// called runtime resolves as yourmodule/pkg/runtime.Func, which does not match.
const runtimePrefix = "runtime."

// noAnchor is the anchor Recover captures with. Nothing of this package sits
// below the panic site there, so there is no bottom trim to run, and passing it
// says so where the capture is asked for.
const noAnchor = ""

// skipSelf drops runtime.Callers, capture, and capture's caller, so the capture
// starts at the code that called in. Both call sites are therefore a function of
// this package invoked directly by the code whose stack is wanted: Recover, or
// contained.
//
// This is the one hard-coded frame count here, and it spans this package's own
// frames, never a caller's. On the panic path a wrong count hides behind the
// gopanic scan, which re-anchors the stack either way. Nothing repairs it on the
// fallback path, so TestRecover_outsideAPanicRetainsTheCallerStack asserts the
// top retained frame is its own; that fails for a count wrong in either
// direction.
//
// That test pins Recover alone, because Recover is the only entry point ever
// exercised off a panic path. One count serving two entry points is sound only
// while the two sit at one depth, which is what
// TestSkipSelf_containedSitsAtRecoverDepth pins.
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
//
// That makes the nil check the caller's to write, and skipping it is worse than
// it looks: `var err error = Recover(recover())` after a clean run holds a nil
// *Panic in a non-nil interface, so err != nil reports a failure, err.Error()
// prints "panic: <nil>", and both Is and As deny it is a panic. Assign through
// the check, or use Catch, which returns error for exactly this reason.
func Recover(recovered any) *Panic {
	if recovered == nil {
		return nil
	}

	return &Panic{Value: recovered, stack: capture(noAnchor)}
}

// contained is Recover for this package's own guards, the ones that put a frame
// of catch.go on the stack below the panic. Only they can be trimmed at the
// bottom, so only they pay for looking: an unassisted Recover would scan the
// whole stack to conclude there is no boundary, which measured as a third of the
// recover cost for nothing.
//
// It carries no nil check of its own. Its one caller has already decided that a
// panic happened, and a nil value does not mean "no panic" there the way it does
// for Recover. See CatchError.
//
// Kept at Recover's call depth, so that skipSelf means one thing at both.
func contained(recovered any) *Panic {
	return &Panic{Value: recovered, stack: capture(catchFile)}
}

// capture records the panicking goroutine's stack, trimmed at both ends: the
// first retained frame is the function that called panic, and the last is the
// deepest frame still on the panic's way out, before this package caught it.
//
// The bottom trim runs only when the anchor names a file to stop at. Recover
// passes noAnchor, because a caller's own deferred function is not something this
// package can recognise; catch.go passes catchFile, which is empty only if the
// runtime declined to answer at init. Both spellings mean the same thing: no
// anchor, no bottom trim. Asking here rather than inside containmentSite is what
// keeps that from being two questions in two places.
//
// See panicSite for the trim at the top and containmentSite for the one at the
// bottom. Both return an index into the frames they were handed.
func capture(anchor string) []uintptr {
	var pcs [maxFrames]uintptr

	depth := runtime.Callers(skipSelf, pcs[:])
	if depth == 0 {
		return nil
	}

	stack := pcs[panicSite(pcs[:depth]):depth]
	if anchor != noAnchor {
		stack = stack[:containmentSite(stack, anchor)]
	}

	return slices.Clone(stack)
}

// panicSite returns the index of the first frame past runtime.gopanic, or 0 when
// the window holds no frame past it: either because gopanic is not there at all,
// or because it took the last slot. The whole capture is retained then.
//
// The miss returns 0 and not len(pcs), the opposite of containmentSite's. Keeping
// everything is what a missing anchor means at this end; at the other end it is
// what len(pcs) means.
//
// Two different situations reach that miss, and only the first is benign. Either
// gopanic is absent because Recover was called outside an unwinding panic, where
// there is no panic site to trim to and every retained frame resolves; or the
// deferred chain between the panic and Recover was long enough to fill the window
// on its own, so the panic site never made it into the capture. The retained
// stack then describes the recovery path rather than the panic. That is a wrong
// answer either way, and the reason it is still the answer is the alternative:
// gopanic landing in the very last slot used to yield pcs[len:len], and a *Panic
// that reports a panic while carrying no evidence of it is worse than one
// carrying the wrong end of the stack. TestCapture_aDeepRecoverSiteStillCarriesFrames
// pins that.
//
// Past this package's own frames (see skipSelf), the panic site is located by
// scanning for runtime.gopanic rather than by a constant. A constant would need
// re-deriving every time a frame is added between the deferred function and this
// one, and a stale one shows up only when somebody reads a bad stack.
//
// The scan itself is unbounded because the distance it covers is the caller's
// choice, not this package's: Recover works from a deferred function at any
// nesting depth the capture window can hold, and every frame in between pushes
// gopanic one further out. A scan bound would cap that depth lower than the
// window already does, without saying so. Past such a bound the trim stops,
// the retained stack leads with runtime.gopanic and this package's own frames,
// and a consumer's dashboard blames the panic on panics instead of on the code
// that raised it. Scanning further costs nothing on a real panic, since the loop
// stops at gopanic; BenchmarkCatch_panic and BenchmarkRecover_deferredAtDepth
// are where that cost is watched.
func panicSite(pcs []uintptr) int {
	for i, pc := range pcs {
		// pc-1 because a return address points just past the call, and the byte
		// after a call can belong to the next function.
		if fn := runtime.FuncForPC(pc - 1); fn != nil && fn.Name() == gopanicName {
			if i+1 == len(pcs) {
				// gopanic took the last slot, so the frame it was found to trim
				// to is not in the window. Finding the anchor and having nothing
				// past it is the same as not finding it.
				//
				// Only Recover can get here, and only from a deferred chain long
				// enough to fill the window on its own. On the contained path
				// gopanic sits at index 1, so reaching this would take a window
				// of two, and skipSelf leaves at least four.
				return 0
			}

			return i + 1
		}
	}

	return 0
}

// containmentSite returns the index at which the panic's own story ends and the
// stack of whoever contained it begins: the first frame belonging to catch.go,
// or len(pcs) when the window holds none.
//
// The boundary is always on the goroutine's stack when this runs, since only this
// package's own guards reach here, but it is not always inside the capture. A
// panic that unwound through more frames than maxFrames pushes the boundary past
// the end of the window, and then nothing is trimmed at the bottom. Nothing needs
// to be: whatever sits below the boundary sits below it in the window as well, so
// a boundary that did not fit means the caller's frames did not either. Such a
// stack comes back truncated mid-unwind rather than run past its end, which is
// what TestCapture_aPanicDeeperThanTheWindowIsTruncatedNotLeaked pins.
//
// Everything from the boundary down is dropped, not just our own frames.
// Removing only ours would leave a hole — the frame above the boundary would
// appear to have been called directly by the frame below it, which it never was.
// And what is below is the caller's own stack: the line where they called Catch,
// their caller, testing.tRunner, runtime.goexit. They already know it, and an
// error-wrapping library on top renders it from its own wrap sites.
//
// FileLine has to run per frame: it is what resolves an inlined frame to the file
// it was written in, and the closure Catch defers is exactly such a frame. So the
// number of frames between the panic site and the guard is the cost, and on a deep
// panic it is the largest thing capture does. A scan from the other end would be
// shorter, our frames being at that end, and it is not here for a reason that
// outranks the saving: it finds the outermost guard, where a Catch nested inside
// a Catch needs the innermost.
//
// That cost grows with depth while what it buys does not: the trim removes the
// same six frames whatever the stack looked like. Measured on an idle
// linux/amd64 box, a shallow panic pays 14% more to capture and reads back 83%
// cheaper, netting 35% for a caller who renders the stack; at depth 32 it is 38%
// more to capture against 23% cheaper to read, which nets out to nothing.
// BenchmarkCatch_panicRendered and BenchmarkCatch_panicDeepStackRendered are the
// pair that says so, and a caller who only checks for a panic and discards it
// sees the capture side alone.
func containmentSite(pcs []uintptr, anchor string) int {
	for i, pc := range pcs {
		site := pc - 1

		fn := runtime.FuncForPC(site)
		if fn == nil {
			continue
		}

		if file, _ := fn.FileLine(site); file != anchor {
			continue
		}

		// The boundary can be the panic site rather than a frame below it: a nil
		// fn is called from catch.go itself, so the runtime raises the panic in
		// the very frame that marks the bottom. Trimming there leaves panicmem,
		// sigpanic, and nothing naming whoever passed the nil. There is no story
		// to keep, so the whole stack is kept instead. That is the answer an
		// unassisted Recover gets too.
		if !anyNonRuntime(pcs[:i]) {
			return len(pcs)
		}

		return i
	}

	return len(pcs)
}

// anyNonRuntime reports whether pcs holds a frame outside the runtime, which is
// how containmentSite separates a panic that travelled to the boundary from one that
// started at it.
//
// Walked from the deepest end, the one nearest the boundary, because that is
// where the code that panicked sits. On a panic raised by a caller's own frame
// the first name resolved is the answer, so this costs one FuncForPC in every
// case but the pathological one; the runtime's own fault frames are at the other
// end, and a forward walk would resolve all of them first.
func anyNonRuntime(pcs []uintptr) bool {
	for _, pc := range slices.Backward(pcs) {
		fn := runtime.FuncForPC(pc - 1)
		if fn != nil && !strings.HasPrefix(fn.Name(), runtimePrefix) {
			return true
		}
	}

	return false
}
