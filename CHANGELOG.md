# Changelog

Notable changes to `panics`. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.0.0 — 2026-08-18

Initial release.

`panics` holds one answer to a question every library that runs caller-supplied
code has to answer separately: what a panic in that code becomes. The answer is
the value passed to `panic` and the stack it came from, and nothing else.

The version is 1.0.0 rather than 0.x deliberately. `*Panic` is meant to be a
shared `errors.As` target across packages, and a target only works if every
package agrees on which module declares it — a major bump would split the
dependency graph the moment two of them disagreed. The API is frozen and grows
only additively; see the README's Design section.

### Added

- **`Catch(fn func()) error` and `CatchError(fn func() error) error`** contain a
  panic raised by `fn` and return it as an ordinary error. `CatchError` carries
  a returned error through unchanged, so one call site handles both outcomes.
- **`Recover(recovered any) *Panic`** is the primitive underneath both, for a
  caller that owns its own `recover()` site. It must run while the panic is
  still unwinding.
- **`Is(err error) bool` and `As(err error) (*Panic, bool)`** are the vocabulary
  a consumer filters on. `As` returns the outermost `*Panic` in the chain and
  walks `Unwrap() error` and `Unwrap() []error`, so it reaches a panic behind
  `errors.Join` or `fmt.Errorf("%w: %w", ...)` rather than only a bare leaf.
- **`ErrPanic`** marks every `Panic`. It is exported so `errors.Is` answers
  correctly on a panic somebody wrapped by hand; `Is` is the way to ask the
  question.
- **`Panic.StackTrace() []uintptr`** returns the frames the panic came from and
  no others. The name and signature are the ones `sentry-go` looks up by
  reflection, so panic frames reach a dashboard without this module depending on
  any SDK.

### Design notes

- **Zero dependencies**, stdlib only.
- **The stack is lazy.** Program counters are captured at recover time and
  symbolicated only when something renders them, so containing a panic nobody
  prints costs 2 allocations rather than a formatted 64-frame string.
- **No caller-dependent skip constant.** The stack is trimmed by locating
  `runtime.gopanic`, so the capture is correct at any nesting depth between a
  caller's deferred function and `Recover`, and the bottom is trimmed at the
  containment boundary.
- **A panic is reported even when its value is nil.** Under `GODEBUG=panicnil=1`,
  or in a main module whose `go` directive predates 1.21, `recover()` still
  yields `nil` for a panic that really happened; treating that as "no panic"
  loses it silently.
- **No formatters, no global state, no `slog` integration.** Rendering belongs
  to an error-wrapping library such as [werr](https://github.com/gokern/werr).
