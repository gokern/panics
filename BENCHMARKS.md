# Benchmarks

One recorded run, kept because `capture.go` reasons about costs and prose that
says "cheaper" without saying how much, on what, is not something a reader can
check. Nothing here is a promise: the ratios below are one machine's, and the
next one moves them by a few points.

Run on 2026-08-18.

## The machine

| | |
|---|---|
| CPU | Intel Xeon Gold 6338 (Ice Lake), 2.00GHz, 4 vCPU as 2 cores × 2 threads |
| Virtualisation | KVM guest |
| Memory | 4GB |
| OS | Debian 11, kernel 5.10.0-46-amd64 |
| Toolchain | go1.26.6 linux/amd64, `GOAMD64=v1` |
| Load | otherwise idle |

## The run

```sh
go test ./... -bench . -benchtime 1s -timeout 0 -run=XXX -cpu 1 -benchmem
```

which is `make bench`. Twelve times against `711970a`, which carries the
containment trim, and twelve against `5ba617c`, the commit before it landed,
alternating between the two so that a machine drifting under load drifts through
both. Compared with `benchstat`; every row below is `p < 0.05` over `n = 12`
unless it says otherwise.

The baseline's own `bench_test.go` predates
`BenchmarkCatch_panicDeepStackRendered` and declares `raiseAtDepth` itself, so
both trees ran this tree's `bench_test.go`, with `raiseAtDepth` handed to the
baseline in a file of its own. Otherwise each tree is its commit.

## What the trim cost and what it bought

| benchmark | before | after | |
|---|---|---|---|
| `Catch_noPanic` | 9.06ns | 8.46ns | −6.7% |
| `Catch_panic` | 1.936µs | 2.228µs | +15.1% |
| `Catch_panicDeepStack` | 4.361µs | 6.139µs | +40.8% |
| `Catch_panicRendered` | 4.488µs | 3.029µs | −32.5% |
| `Catch_panicDeepStackRendered` | 10.80µs | 11.07µs | +2.5% |
| `Recover_deferredAtDepth` | 3.111µs | 3.101µs | ~ (p=0.225) |
| `Is_wrapped` | 51.8ns | 53.2ns | +2.7% |

Allocation counts are identical on every benchmark. The bytes are not, the
retained stack being shorter: `Catch_panic` 112B → 56B, `Catch_panicDeepStack`
400B → 336B, `Catch_panicRendered` 416B → 304B,
`Catch_panicDeepStackRendered` 992B → 864B.

`Catch_noPanic` has no capture in it either way, and neither trim runs on a
clean return; the 6.7% is code layout, not work removed.

## Frames

What the capture retains, which is where the direction of everything above comes
from:

| | before | after |
|---|---|---|
| shallow panic | 7 | 1 |
| panic at depth 32 | 40 | 34 |

Six frames at both depths, as `containmentSite` says: this package's `Catch`,
`CatchError` and the closure between them, the caller's line, `testing.tRunner`,
`runtime.goexit`.

This table is the one thing here that is not a measurement. The counts are
deterministic, `TestCapture_stopsAtTheContainmentBoundary` pins the retained
side of them, and they are the same on any machine — which is why the comments
in the code state them and leave the timings to this file.

## The read side

`Catch_panicRendered` less `Catch_panic` is what a caller pays to walk the
retained stack, and it is derived rather than benchmarked:

| | before | after | |
|---|---|---|---|
| shallow | 2.552µs | 0.800µs | −68.6% |
| depth 32 | 6.438µs | 4.926µs | −23.5% |

The method matters more than it looks. A benchmark that captures once outside
the loop and renders inside it puts the same two figures at −87.7% and −30.5%,
because the subtraction above charges the render side with the difference
between two capture costs as well. The derived numbers are the ones quoted,
since they are the ones that add up to the net rows in the first table.

## Recover pays nothing for the boundary it cannot have

`contained` exists as a separate entry point so that `Recover` never scans for a
containment boundary that a hand-written deferred function cannot have. Patching
`Recover` to pass `catchFile` instead of `noAnchor`, so that the scan runs and
finds nothing:

| benchmark | as shipped | scanning | |
|---|---|---|---|
| `Recover_deferredAtDepth` | 3.097µs | 4.184µs | +35.1% |

A third of the cost of a recover, for a boundary that is not there.

## Reproducing

```sh
git worktree add ../panics-before 5ba617c
cp bench_test.go ../panics-before/bench_test.go
# the baseline needs raiseAtDepth, which this tree keeps in fixtures_test.go
sed -n '/^\/\/ raiseAtDepth calls itself/,/^}/p' fixtures_test.go \
    | cat <(printf 'package panics_test\n\n') - > ../panics-before/depth_test.go

for i in $(seq 1 12); do
    (cd ../panics-before && make bench) >> before.txt
    make bench >> after.txt
done

benchstat before.txt after.txt
```
