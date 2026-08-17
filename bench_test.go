package panics_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/gokern/panics"
)

// These benchmarks measure the cost claims the package documentation makes.

// Sinks keep the benchmarked results from being optimised away without adding a
// per-iteration assignment the compiler could hoist.
//
//nolint:gochecknoglobals // escape sinks; a local would let the compiler drop the measured work.
var (
	errSink      error
	boolSink     bool
	frameSink    int
	errPanicSink *panics.Panic
)

//go:noinline
func raiseAtDepth(n int) {
	if n == 0 {
		panic("boom")
	}

	raiseAtDepth(n - 1)
}

// The price every caller pays on every call, panic or not. It must stay free of a
// capture.
func BenchmarkCatch_noPanic(b *testing.B) {
	for b.Loop() {
		errSink = panics.Catch(func() {})
	}
}

// One capture: runtime.Callers, the scan for runtime.gopanic, and one
// slices.Clone of the trimmed stack. No symbol resolution.
func BenchmarkCatch_panic(b *testing.B) {
	for b.Loop() {
		errSink = panics.Catch(raise)
	}
}

// The same capture from a deep stack. The scan resolves one function name per
// frame until it finds gopanic, so a deep recover site pays more than a shallow
// one.
func BenchmarkCatch_panicDeepStack(b *testing.B) {
	for b.Loop() {
		errSink = panics.Catch(func() { raiseAtDepth(32) })
	}
}

// The cost the lazy stack defers. The gap between this and BenchmarkCatch_panic
// is what a caller who checks a panic and discards it never pays.
func BenchmarkCatch_panicRendered(b *testing.B) {
	for b.Loop() {
		err := panics.Catch(raise)

		p, ok := panics.As(err)
		if !ok {
			b.Fatal("Catch must report the panic")
		}

		frames := runtime.CallersFrames(p.StackTrace())
		count := 0

		for {
			frame, more := frames.Next()
			count += len(frame.Function)

			if !more {
				break
			}
		}

		frameSink = count
	}
}

// The shape the unbounded gopanic scan is for: a deferred function that hands the
// recovered value down through several frames before Recover sees it, so gopanic
// sits past where a bounded scan would stop looking. That makes this the
// legitimate shape where scanning further could cost something, and it does not:
// the scan still stops at gopanic. The fixture is capture_test.go's deepDefer.
func BenchmarkRecover_deferredAtDepth(b *testing.B) {
	for b.Loop() {
		errPanicSink = deepDefer(12)
	}
}

// Is on a wrapped panic, where every visited node allocates: Unwrap builds a
// fresh two-element slice per call, and errors.Is calls it once per node.
func BenchmarkIs_wrapped(b *testing.B) {
	err := fmt.Errorf("outer: %w", panics.Catch(raise))

	for b.Loop() {
		boolSink = panics.Is(err)
	}
}
