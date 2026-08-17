package panics

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
)

// ErrPanic marks every Panic. It is the one check that holds across every
// package that recovers through this one:
//
//	if errors.Is(err, panics.ErrPanic) { ... }
//
// A package with its own sentinel wraps this one rather than replacing it, so
// both checks hold on the same error.
var ErrPanic = errors.New("panic")

// Panic is a recovered panic: the value passed to panic, and the stack it came
// from.
//
// The stack is a field rather than part of the message. An error string is one
// line by convention, and a multi-kilobyte stack inside one fragments every log
// aggregator that treats a line as a record.
//
// Keeping the stack out only makes that convention possible to hold. Error
// renders Value verbatim, so a panic value containing a newline still produces a
// message with one. Value is the raw access point for a caller that needs to
// escape or flatten it.
type Panic struct { //nolint:errname // frozen v1 name; see the package doc.
	Value any

	stack []uintptr
}

// Every method below tolerates a nil receiver. Other modules use this type as a
// shared errors.As target (see the package doc), so a typed-nil *Panic can end up
// in a chain built by code this package never sees, and both walks that matter
// visit every node of one: errors.Is calls Unwrap on each, sentry-go's
// convertErrorDFS calls Error on each. A nil receiver has to be inert in both
// rather than a crash far from its cause. [As] refuses a typed nil for the same
// reason and cannot lean on these guards, since no method protects a read of the
// exported Value field.

// Error renders the panic as "panic: <value>". A nil *Panic renders as
// "panic: <nil>", which is what a nil Value renders as too.
func (p *Panic) Error() string {
	if p == nil {
		return "panic: <nil>"
	}

	return "panic: " + valueString(p.Value)
}

// StackTrace returns the program counters of the frames the panic came from,
// innermost first, ready for runtime.CallersFrames.
//
// The name and signature are the ones sentry-go looks up by reflection, so panic
// frames reach an APM dashboard without this package depending on any SDK.
//
// The result is a copy, so a caller cannot corrupt the error for whoever reads it
// next. A nil *Panic has no frames and returns nil.
func (p *Panic) StackTrace() []uintptr {
	if p == nil {
		return nil
	}

	return slices.Clone(p.stack)
}

// Unwrap reports ErrPanic, and the panic value too when it is a usable error.
// A nil *Panic marks nothing and returns nil, which ends the walk there.
//
// A typed-nil error is withheld: it would satisfy errors.As for its type and
// then nil-deref in whatever reads it. valueString keeps it visible in the
// message instead.
//
// The slice is built fresh per call, so an errors.Is or errors.As allocates one
// small slice for every Panic its walk reaches. It cannot be built once and
// reused, because Value is exported and mutable: a cached list could name a cause
// the Panic no longer carries.
func (p *Panic) Unwrap() []error {
	if p == nil {
		return nil
	}

	cause, ok := p.Value.(error)
	if !ok || isTypedNil(p.Value) {
		return []error{ErrPanic}
	}

	return []error{ErrPanic, cause}
}

// valueString renders a panic value for Error.
//
// A typed nil that satisfies error, such as panic((*os.PathError)(nil)) or a nil
// func type with an Error method, is a non-nil interface holding a nil dynamic
// value. Calling Error on it nil-derefs, so it renders with %#v instead and the
// panic stays observable.
//
// Everything else goes through fmt, which runs Error, String and GoString under
// its own recover. A value whose rendering panics for a reason no guard here can
// predict (a non-nil struct with a nil field it dereferences, say) then comes out
// as "%!v(PANIC=Error method: ...)" rather than panicking out of a package whose
// job is containing panics.
func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case error:
		if isTypedNil(value) {
			return fmt.Sprintf("%#v", value)
		}

		return fmt.Sprintf("%v", typed)
	default:
		return fmt.Sprintf("%#v", typed)
	}
}

// isTypedNil reports whether value is a non-nil interface holding a nil dynamic
// value.
//
// The kinds listed are the nilable ones that can satisfy error.
// reflect.Value.IsNil panics on any kind outside its own accepted set, which is
// the panic this function exists to avoid. It accepts two more kinds that are
// left out here, and neither can reach this far: value arrives as an any, so its
// dynamic kind is never Interface, and an UnsafePointer has no methods, so it
// cannot satisfy error.
func isTypedNil(value any) bool {
	rv := reflect.ValueOf(value)

	switch rv.Kind() { //nolint:exhaustive // only nilable kinds are relevant.
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
