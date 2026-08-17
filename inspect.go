package panics

import "errors"

// Is reports whether err is, or wraps, a recovered panic. It is
// errors.Is(err, ErrPanic) under a name that says what the check means.
func Is(err error) bool {
	return errors.Is(err, ErrPanic)
}

// As returns the outermost *Panic in err's chain, and whether there was one. It
// reaches through package wrappers, so a caller holding an error from any package
// that recovers through this one can get to Value and StackTrace.
//
// Outermost means the first one the walk meets coming from err, which is what
// errors.As reports for any other type and is the panic that was contained: one
// *Panic per recovery, and the wrappers around it belong to whoever caught it. A
// chain holding more than one is pathological, a recovered panic re-raised and
// recovered again, and there the result describes the outer recovery, whose Value
// is the inner *Panic. Unwrap keeps the inner one reachable for a caller that
// cares.
//
// A typed-nil *Panic is skipped rather than reported. A type assertion succeeds on
// one, so reporting it would make As report success and hand back a pointer that
// nil-derefs on the very first Value read. The walk then carries on past the typed
// nil to whatever else the chain holds, because a caller told by Is that the error
// is a panic must be able to reach that panic through As.
func As(err error) (*Panic, bool) {
	// The assertions below are deliberately concrete rather than errors.As or
	// errors.AsType: this function *is* the walk, and delegating the match to one
	// of those would hand back the first node whose type fits, a typed nil, and
	// give up there, which is the bug this shape exists to avoid.
	for err != nil {
		p, ok := err.(*Panic) //nolint:errorlint // see above.
		if ok && p != nil {
			return p, true
		}

		switch unwrapper := err.(type) { //nolint:errorlint // see above.
		case interface{ Unwrap() error }:
			err = unwrapper.Unwrap()
		case interface{ Unwrap() []error }:
			// A tree: the first branch holding a usable *Panic wins. A typed-nil
			// *Panic lands here too, with an Unwrap of nil, so it contributes no
			// branches and the caller's loop moves on to its siblings.
			for _, branch := range unwrapper.Unwrap() {
				if p, ok := As(branch); ok {
					return p, true
				}
			}

			return nil, false
		default:
			return nil, false
		}
	}

	return nil, false
}
