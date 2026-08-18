package panics_test

// The functions this package's tests panic through. They sit here rather than
// beside a first caller because the callers are spread across three files, and a
// fixture declared in one test file while another reads it leaves neither
// readable on its own.
//
// //go:noinline throughout: what these tests read is the shape of the stack each
// fixture appears in, so every one of them has to be a frame of its own.

//go:noinline
func raise() { panic("boom") }

//go:noinline
func raiseNil() { panic(nil) }

// raiseAtDepth calls itself down to n == 0 and panics there, which makes the
// number of frames between the panic site and whatever contains it a parameter
// of the test rather than a property of how a fixture happens to be written.
//
//go:noinline
func raiseAtDepth(n int) {
	if n == 0 {
		panic("boom")
	}

	raiseAtDepth(n - 1)
}

// The three below are faults the runtime raises rather than panics the code
// called panic for, which puts frames of runtime.* above the one that faulted.
// They are what stops the containment trim from standing down for every panic
// that merely leads with a runtime frame. The mistakes in them are deliberate,
// and each is written the way it actually reaches a consumer: as ordinary code
// that is wrong.

//go:noinline
func raiseIndexOutOfRange() {
	empty := []int{}

	_ = empty[5] //nolint:gosec // G602: the out-of-range index is the fault being raised.
}

//go:noinline
func raiseNilMapWrite() {
	var unmade map[int]int

	unmade[1] = 1 //nolint:staticcheck // SA5000: the nil map write is the fault being raised.
}

//go:noinline
func raiseDivideByZero() {
	zero := 0

	_ = 1 / zero
}
