// Package panics is the minimal representation of a recovered panic.
//
// A library that runs code it did not write has to decide what a panic in that
// code becomes. This package holds the answer every gokern package shares: what
// was passed to panic, and where it happened.
//
//	if err := panics.Catch(userCallback); err != nil {
//	    // errors.Is(err, panics.ErrPanic) holds.
//	}
//
// Stacks are kept as program counters and symbolicated only when something
// renders them, so a panic that is checked and discarded never pays for the
// expensive part.
//
// There are deliberately no formatters here, no global state, no slog
// integration, and no slot for a package-specific sentinel or call site.
// Rendering belongs to an error-wrapping library. Classifying a panic and naming
// its site belong to whoever caught it.
//
// Several modules use [Panic] as a shared errors.As target, so a v2 would split
// the dependency graph. The v1 API is frozen and grows only additively.
package panics
