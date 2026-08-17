package panics

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

		p := Recover(recovered)
		if p != nil {
			err = p

			return
		}

		if !completed {
			// recover() yielded nil yet fn never returned: a panic(nil) under
			// panicnil=1. The recover above already consumed it, so reporting it
			// here is the only way it is not lost without trace.
			err = &Panic{Value: recovered, stack: capture()}
		}
	}()

	err = fn()
	completed = true

	return err
}
