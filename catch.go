package panics

import "runtime"

// catchFile is this file's path as the compiler recorded it, and it anchors the
// bottom trim the way runtime.gopanic anchors the top: a frame from here is
// containment machinery, not a step the panic took on its way out.
//
// This file and no other. Recover, contained and capture live in capture.go and
// always sit above gopanic, so the top trim has already dropped them; the only
// frames of ours that can appear below the panic site are Catch, CatchError, and
// the closure between them, all three declared here.
//
// Matched on the file rather than the function name, because the name does not
// identify us: Catch hands CatchError a closure the compiler attributes to
// whoever called Catch, spelled "yourpkg.YourFunc.Catch.func1". A name test
// walks straight past the frame that marks the boundary. The file is catch.go
// either way.
//
// Read from runtime.Caller rather than written down, so it survives -trimpath
// (which rewrites this path and the frames' paths identically), a module rename,
// and vendoring. Compared exactly, not by directory, so the package's own tests
// — which sit in the same directory and do the panicking — are not mistaken for
// machinery. Empty only if the runtime declines to answer, which capture reads
// as noAnchor and answers by not trimming the bottom at all.
//
//nolint:gochecknoglobals // one compiler-supplied constant, resolved once.
var catchFile = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	return file
}()

// Catch runs fn and contains a panic raised by it, returning the panic as an
// error or nil:
//
//	if err := panics.Catch(func() { observer.Published(ctx, delivery, err) }); err != nil {
//	    report(err)
//	}
//
// A library that runs code it did not write is the intended caller. The panic
// stops at this boundary instead of reaching the runtime and taking every other
// in-flight operation with it.
//
// The return type is error and not *Panic because the result flows onward, where
// `var err error = Catch(fn)` would otherwise store a nil pointer in a non-nil
// interface. Callers that need the frames use As.
func Catch(fn func()) error {
	return CatchError(func() error {
		fn()

		return nil
	})
}

// CatchError runs fn and returns its error, or, if fn panicked, the recovered
// panic as an error. It is the result-carrying form of Catch, for callers whose
// function already reports failure the ordinary way. A returned error is passed
// through untouched, so errors.Is(err, ErrPanic) distinguishes the two outcomes.
//
// A panic is reported even when its value is nil. Go 1.21 turned panic(nil) into
// a *runtime.PanicNilError, but GODEBUG=panicnil=1 and a main module whose go
// directive predates 1.21 both restore the old behaviour, where recover() yields
// nil for a panic that really happened. Such a panic reports as "panic: <nil>",
// the same rendering a nil Value has anywhere else.
func CatchError(fn func() error) (err error) {
	completed := false

	defer func() {
		recovered := recover()

		// A nil value with fn having returned is the only clean run. A nil value
		// with fn never returning is a panic(nil) under panicnil=1: the recover
		// above already consumed it, so reporting it here is the only way it is
		// not lost without trace.
		if recovered == nil && completed {
			return
		}

		err = contained(recovered)
	}()

	err = fn()
	completed = true

	return err
}
